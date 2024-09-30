// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package remotecluster

import (
	"context"

	"k8s.io/apimachinery/pkg/util/sets"

	esv1 "github.com/elastic/cloud-on-k8s/v2/pkg/apis/elasticsearch/v1"
	"github.com/elastic/cloud-on-k8s/v2/pkg/controller/remotecluster"
	"github.com/elastic/cloud-on-k8s/v2/pkg/utils/k8s"
)

// garbageCollectAPIKeys deletes the API keys which are no longer used from the local keystore.
func garbageCollectAPIKeys(
	ctx context.Context,
	c k8s.Client,
	es *esv1.Elasticsearch) error {

	apiKeyStore, err := remotecluster.LoadAPIKeyStore(ctx, c, es)
	if err != nil {
		return err
	}
	if apiKeyStore.IsEmpty() {
		return nil
	}

	expectedKeys := sets.New[string]()
	for _, remoteCluster := range es.Spec.RemoteClusters {
		if remoteCluster.APIKey != nil {
			expectedKeys.Insert(remoteCluster.Name)
		}
	}

	changed := false
	for _, alias := range apiKeyStore.Aliases() {
		if !expectedKeys.Has(alias) {
			apiKeyStore.Delete(alias)
			changed = true
		}
	}

	if !changed {
		return nil
	}

	return apiKeyStore.Save(ctx, c, es)
}
