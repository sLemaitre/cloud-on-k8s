// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package remotecluster

import (
	"context"
	"fmt"
	"github.com/elastic/cloud-on-k8s/v2/pkg/controller/common/hash"
	"github.com/elastic/cloud-on-k8s/v2/pkg/utils/k8s"
	"k8s.io/apimachinery/pkg/util/sets"

	esv1 "github.com/elastic/cloud-on-k8s/v2/pkg/apis/elasticsearch/v1"
	esclient "github.com/elastic/cloud-on-k8s/v2/pkg/controller/elasticsearch/client"
)

// reconcileAPIKeys creates or updates the API Keys for the remoteEs cluster,
// which may have several references (remoteClusters) to the cluster being reconciled.
func reconcileAPIKeys(
	ctx context.Context,
	c k8s.Client,
	activeAPIKeys esclient.CrossClusterAPIKeyList, // all the API Keys in the reconciled/local cluster
	reconciledES *esv1.Elasticsearch, // the Elasticsearch cluster being reconciled, where the API keys must be created/invalidated.
	clientES *esv1.Elasticsearch, // the remote Elasticsearch cluster which is going to act as the client, where the API keys are going to be store in the keystore Secret.
	remoteClusters []esv1.RemoteCluster, // the expected API keys for that client cluster
	esClient esclient.Client, // ES client for the remote cluster which is going to act as the client
) error {
	// We may have to inject new API keystore in the client keystore.
	apiKeyStore, err := LoadAPIKeyStore(ctx, c, clientES)
	if err != nil {
		return err
	}

	// Maintain a list of the expected API keys to detect the ones which are no longer expected in the reconciled cluster.
	expectedKeys := sets.New[string]()
	for _, remoteCluster := range remoteClusters {
		apiKeyName := fmt.Sprintf("eck-%s-%s-%s", clientES.Namespace, clientES.Name, remoteCluster.Name)
		expectedKeys.Insert(apiKeyName)
		if remoteCluster.APIKey == nil {
			// This cluster is not configure with API keys, attempt to invalidate any key and continue.
			if err := esClient.InvalidateCrossClusterAPIKey(ctx, apiKeyName); err != nil {
				return err
			}
			continue
		}
		// 1. Attempt to get the API Key
		activeAPIKey := activeAPIKeys.GetActiveKeyWithName(apiKeyName)
		// 2.1 If not exist create it in the reconciled cluster.
		expectedHash := hash.HashObject(remoteCluster.APIKey)
		if activeAPIKey == nil {
			apiKey, err := esClient.CreateCrossClusterAPIKey(ctx, esclient.CrossClusterAPIKeyCreateRequest{
				Name: apiKeyName,
				CrossClusterAPIKeyUpdateRequest: esclient.CrossClusterAPIKeyUpdateRequest{
					RemoteClusterAPIKey: *remoteCluster.APIKey,
					Metadata: map[string]interface{}{
						"elasticsearch.k8s.elastic.co/config-hash": expectedHash,
						"elasticsearch.k8s.elastic.co/name":        clientES.Name,
						"elasticsearch.k8s.elastic.co/namespace":   clientES.Namespace,
						"elasticsearch.k8s.elastic.co/uid":         clientES.UID,
						"elasticsearch.k8s.elastic.co/managed-by":  "eck",
					},
				},
			})
			if err != nil {
				return err
			}
			apiKeyStore.Update(reconciledES.Name, reconciledES.Namespace, remoteCluster.Name, apiKey.ID, apiKey.Encoded)
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
						"elasticsearch.k8s.elastic.co/name":        clientES.Name,
						"elasticsearch.k8s.elastic.co/namespace":   clientES.Namespace,
						"elasticsearch.k8s.elastic.co/uid":         clientES.UID,
						"elasticsearch.k8s.elastic.co/managed-by":  "eck",
					},
				})
				if err != nil {
					return err
				}
			}
		}
	}

	// Delete all the keys related to that local cluster which are not expected.
	for keyName := range activeAPIKeys.KeyNames() {
		if !expectedKeys.Has(keyName) {
			// Unexpected key let's invalidate it.
			if err := esClient.InvalidateCrossClusterAPIKey(ctx, keyName); err != nil {
				return err
			}
		}
	}

	// Save the generated keys in the keystore.
	if err := apiKeyStore.Save(ctx, c, clientES); err != nil {
		return err
	}
	return nil
}
