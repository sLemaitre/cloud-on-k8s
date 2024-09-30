// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package remotecluster

import (
	"context"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"

	esv1 "github.com/elastic/cloud-on-k8s/v2/pkg/apis/elasticsearch/v1"
	"github.com/elastic/cloud-on-k8s/v2/pkg/utils/k8s"
)

const (
	testNamespace = "ns1"
)

var (
	es = &esv1.Elasticsearch{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "myes",
			Namespace:  testNamespace,
			UID:        uuid.NewUUID(),
			Generation: 42,
		},
		Spec: esv1.ElasticsearchSpec{},
	}
)

func TestLoadAPIKeyStore(t *testing.T) {
	type args struct {
		c     k8s.Client
		owner *esv1.Elasticsearch
	}
	tests := []struct {
		name    string
		args    args
		want    *APIKeyStore
		wantErr bool
	}{
		{
			name: "Happy path",
			args: args{
				c: k8s.NewFakeClient(
					&corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: testNamespace,
							Name:      "myes-es-remote-api-keys",
							Annotations: map[string]string{
								"elasticsearch.k8s.elastic.co/remote-clusters-keys": `{ "rc2" : "SecretKeyID2", "rc1" : "SecretKeyID1"}`,
							},
						},
						Data: map[string][]byte{
							"cluster.remote.rc1.credentials": []byte("SecretKeyValue1"),
							"cluster.remote.rc2.credentials": []byte("SecretKeyValue2"),
						},
					},
				),
				owner: es,
			},
			want: &APIKeyStore{
				aliases: map[string]string{
					"rc1": "SecretKeyID1",
					"rc2": "SecretKeyID2",
				},
				keys: map[string]string{
					"rc1": "SecretKeyValue1",
					"rc2": "SecretKeyValue2",
				},
			},
		},
		{
			name: "Secret does not exist",
			args: args{
				c:     k8s.NewFakeClient(),
				owner: es,
			},
			want: &APIKeyStore{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			got, err := LoadAPIKeyStore(ctx, tt.args.c, tt.args.owner)
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadAPIKeyStore() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("LoadAPIKeyStore() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAPIKeyStore_Save(t *testing.T) {
	type args struct {
		c k8s.Client
	}
	tests := []struct {
		name     string
		receiver *APIKeyStore
		args     args
		wantErr  bool
		want     *corev1.Secret
	}{
		{
			name: "Create a new store",
			receiver: (&APIKeyStore{}).
				Update("rc1", "keyid1", "encodedValue1").
				Update("rc2", "keyid2", "encodedValue2"),
			args: args{c: k8s.NewFakeClient()},
			want: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:       testNamespace,
					Name:            "myes-es-remote-api-keys",
					ResourceVersion: "1",
					Annotations: map[string]string{
						"elasticsearch.k8s.elastic.co/remote-clusters-keys": `{"rc1":"keyid1","rc2":"keyid2"}`,
					},
					Labels: map[string]string{
						"common.k8s.elastic.co/type":                "elasticsearch",
						"eck.k8s.elastic.co/credentials":            "true",
						"elasticsearch.k8s.elastic.co/cluster-name": "myes",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "elasticsearch.k8s.elastic.co/v1",
							Kind:               "Elasticsearch",
							Name:               "myes",
							UID:                es.UID,
							Controller:         ptr.To(true),
							BlockOwnerDeletion: ptr.To(true),
						},
					},
				},
				Data: map[string][]byte{
					"cluster.remote.rc1.credentials": []byte("encodedValue1"),
					"cluster.remote.rc2.credentials": []byte("encodedValue2"),
				},
			},
		},
		{
			name: "Delete the store",
			receiver: (&APIKeyStore{}).
				Update("rc1", "keyid1", "encodedValue1").
				Update("rc2", "keyid2", "encodedValue2").
				Delete("rc1").Delete("rc2"),
			args: args{c: k8s.NewFakeClient(&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:       testNamespace,
					Name:            "myes-es-remote-api-keys",
					ResourceVersion: "1",
					Annotations: map[string]string{
						"elasticsearch.k8s.elastic.co/remote-clusters-keys": `{"rc1":"keyid1","rc2":"keyid2"}`,
					},
					Labels: map[string]string{
						"common.k8s.elastic.co/type":                "elasticsearch",
						"eck.k8s.elastic.co/credentials":            "true",
						"elasticsearch.k8s.elastic.co/cluster-name": "myes",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "elasticsearch.k8s.elastic.co/v1",
							Kind:               "Elasticsearch",
							Name:               "myes",
							UID:                es.UID,
							Controller:         ptr.To(true),
							BlockOwnerDeletion: ptr.To(true),
						},
					},
				},
				Data: map[string][]byte{
					"cluster.remote.rc1.credentials": []byte("encodedValue1"),
					"cluster.remote.rc2.credentials": []byte("encodedValue2"),
				},
			})},
		},
		{
			name: "Add a new key, remove another",
			receiver: (&APIKeyStore{}).
				Update("rc1", "keyid1", "encodedValue1").
				Update("rc2", "keyid2", "encodedValue2").
				Delete("rc1").Update("rc3", "keyid3", "encodedValue3"),
			args: args{c: k8s.NewFakeClient(&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:       testNamespace,
					Name:            "myes-es-remote-api-keys",
					ResourceVersion: "1",
					Annotations: map[string]string{
						"elasticsearch.k8s.elastic.co/remote-clusters-keys": `{"rc1":"keyid1","rc2":"keyid2"}`,
					},
					Labels: map[string]string{
						"common.k8s.elastic.co/type":                "elasticsearch",
						"eck.k8s.elastic.co/credentials":            "true",
						"elasticsearch.k8s.elastic.co/cluster-name": "myes",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "elasticsearch.k8s.elastic.co/v1",
							Kind:               "Elasticsearch",
							Name:               "myes",
							UID:                es.UID,
							Controller:         ptr.To(true),
							BlockOwnerDeletion: ptr.To(true),
						},
					},
				},
				Data: map[string][]byte{
					"cluster.remote.rc1.credentials": []byte("encodedValue1"),
					"unexpected_key":                 []byte("foo"),
					"cluster.remote.rc2.credentials": []byte("encodedValue2"),
				},
			})},
			want: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:       testNamespace,
					Name:            "myes-es-remote-api-keys",
					ResourceVersion: "2",
					Annotations: map[string]string{
						"elasticsearch.k8s.elastic.co/remote-clusters-keys": `{"rc2":"keyid2","rc3":"keyid3"}`,
					},
					Labels: map[string]string{
						"common.k8s.elastic.co/type":                "elasticsearch",
						"eck.k8s.elastic.co/credentials":            "true",
						"elasticsearch.k8s.elastic.co/cluster-name": "myes",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "elasticsearch.k8s.elastic.co/v1",
							Kind:               "Elasticsearch",
							Name:               "myes",
							UID:                es.UID,
							Controller:         ptr.To(true),
							BlockOwnerDeletion: ptr.To(true),
						},
					},
				},
				Data: map[string][]byte{
					"cluster.remote.rc2.credentials": []byte("encodedValue2"),
					"cluster.remote.rc3.credentials": []byte("encodedValue3"),
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			owner := es.DeepCopy()
			if err := tt.receiver.Save(ctx, tt.args.c, owner); (err != nil) != tt.wantErr {
				t.Errorf("APIKeyStore.Save() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.want != nil {
				// get the Secret
				apiKeysSecret := corev1.Secret{}
				assert.NoError(t, tt.args.c.Get(ctx, types.NamespacedName{Name: "myes-es-remote-api-keys", Namespace: testNamespace}, &apiKeysSecret))
				if diff := cmp.Diff(*tt.want, apiKeysSecret); len(diff) > 0 {
					t.Errorf("%s", diff)
				}
			} else {
				// ensure the Secret does not exist
				err := tt.args.c.Get(ctx, types.NamespacedName{Name: "myes-es-remote-api-keys", Namespace: testNamespace}, &corev1.Secret{})
				assert.Truef(t, errors.IsNotFound(err), "expected a 404 error")
			}
		})
	}
}
