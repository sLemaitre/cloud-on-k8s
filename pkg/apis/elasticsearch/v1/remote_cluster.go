// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package v1

// RemoteClusterAPIKey defines a remote cluster API Key.
type RemoteClusterAPIKey struct {
	// Expiration date. If set the key is automatically renewed by ECK.
	//Expiration *metav1.Duration `json:"name,omitempty"`

	// Access is the name of the API Key. It is automatically generated if not set or empty.
	// +kubebuilder:validation:Required
	Access RemoteClusterAccess `json:"access,omitempty"`
}

type RemoteClusterAccess struct {
	// +kubebuilder:validation:Optional
	Search *Search `json:"search,omitempty"`
	// +kubebuilder:validation:Optional
	Replication *Replication `json:"replication,omitempty"`
}

type Search struct {
	// +kubebuilder:validation:Required
	Names []string `json:"names,omitempty"`

	// +kubebuilder:validation:Optional
	FieldSecurity *FieldSecurity `json:"field_security,omitempty"`
}

type FieldSecurity struct {
	Grant  []string `json:"grant"`
	Except []string `json:"except"`
}

type Replication struct {
	// +kubebuilder:validation:Required
	Names []string `json:"names,omitempty"`
}
