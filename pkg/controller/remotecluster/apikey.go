// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package remotecluster

import (
	"context"
	"fmt"
	"github.com/elastic/cloud-on-k8s/v2/pkg/controller/common/hash"
	"github.com/elastic/cloud-on-k8s/v2/pkg/utils/k8s"

	esv1 "github.com/elastic/cloud-on-k8s/v2/pkg/apis/elasticsearch/v1"
	esclient "github.com/elastic/cloud-on-k8s/v2/pkg/controller/elasticsearch/client"
)

// reconcileAPIKeys creates or updates the API Keys for the remoteEs cluster,
// which may have several references (remoteClusters) to the cluster being reconciled.
func reconcileAPIKeys(
	ctx context.Context,
	c k8s.Client,
	localES *esv1.Elasticsearch,
	remoteEs *esv1.Elasticsearch, // the remote cluster which is going to act as the client
	remoteClusters []esv1.RemoteCluster, // the API keys
	esClient esclient.Client,
) error {
	// Create the API Keys if needed
	apiKeyStore, err := LoadAPIKeyStore(ctx, c, remoteEs)
	if err != nil {
		return err
	}
	for _, remoteCluster := range remoteClusters {
		apiKeyName := fmt.Sprintf("eck-%s-%s-%s", remoteEs.Namespace, remoteEs.Name, remoteCluster.Name)
		if remoteCluster.APIKey == nil {
			// This cluster is not configure with API keys, attempt to invalidate any key and continue.
			if err := esClient.InvalidateCrossClusterAPIKey(ctx, apiKeyName); err != nil {
				return err
			}
			continue
		}
		// 1. Get the API Key
		activeAPIKeys, err := esClient.GetCrossClusterAPIKeys(ctx, apiKeyName)
		if err != nil {
			return err

		}
		activeAPIKey, err := activeAPIKeys.GetActiveKey()
		if err != nil {
			return fmt.Errorf("error while retrieving active API Key for remote cluster %s: %w", remoteCluster.Name, err)
		}
		// 2.1 If not exist create it
		expectedHash := hash.HashObject(remoteCluster.APIKey)
		if activeAPIKey == nil {
			apiKey, err := esClient.CreateCrossClusterAPIKey(ctx, esclient.CrossClusterAPIKeyCreateRequest{
				Name: apiKeyName,
				CrossClusterAPIKeyUpdateRequest: esclient.CrossClusterAPIKeyUpdateRequest{
					RemoteClusterAPIKey: *remoteCluster.APIKey,
					Metadata: map[string]interface{}{
						"elasticsearch.k8s.elastic.co/config-hash": expectedHash,
						"elasticsearch.k8s.elastic.co/name":        remoteEs.Name,
						"elasticsearch.k8s.elastic.co/namespace":   remoteEs.Namespace,
						"elasticsearch.k8s.elastic.co/uid":         remoteEs.UID,
					},
				},
			})
			if err != nil {
				return err
			}
			apiKeyStore.Update(localES.Name, localES.Namespace, remoteCluster.Name, apiKey.ID, apiKey.Encoded)
		}
		// 2.2 If exists ensure that the access field is the expected one using the hash
		if activeAPIKey != nil {
			// Ensure that the API key is in the keystore
			if apiKeyStore.KeyIDFor(remoteCluster.Name) != activeAPIKey.ID {
				// We have a problem here, the API Key ID in Elasticsearch does not match the API Key recorded in the Secret.
				// Invalidate the API Key in ES and requeue
				if err := esClient.InvalidateCrossClusterAPIKey(ctx, activeAPIKey.Name); err != nil {
					return err
				}
				return fmt.Errorf("unknwown remote cluster key id for %s: %s", activeAPIKey.Name, activeAPIKey.ID)
			}
			currentHash := activeAPIKey.Metadata["elasticsearch.k8s.elastic.co/config-hash"]
			if currentHash != expectedHash {
				// Update the Key
				_, err := esClient.UpdateCrossClusterAPIKey(ctx, activeAPIKey.ID, esclient.CrossClusterAPIKeyUpdateRequest{
					RemoteClusterAPIKey: *remoteCluster.APIKey,
					Metadata: map[string]interface{}{
						"elasticsearch.k8s.elastic.co/config-hash": expectedHash,
						"elasticsearch.k8s.elastic.co/name":        remoteEs.Name,
						"elasticsearch.k8s.elastic.co/namespace":   remoteEs.Namespace,
						"elasticsearch.k8s.elastic.co/uid":         remoteEs.UID,
					},
				})
				if err != nil {
					return err
				}
			}
		}
		if err := apiKeyStore.Save(ctx, c, remoteEs); err != nil {
			return err
		}
	}
	return nil
}
