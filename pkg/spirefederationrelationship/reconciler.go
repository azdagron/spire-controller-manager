/*
Copyright 2021 SPIRE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package spirefederationrelationship

import (
	"context"
	"time"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	spirev1alpha1 "github.com/spiffe/spire-controller-manager/api/v1alpha1"
	"github.com/spiffe/spire-controller-manager/pkg/k8sapi"
	"github.com/spiffe/spire-controller-manager/pkg/reconciler"
	"github.com/spiffe/spire-controller-manager/pkg/spireapi"
	"google.golang.org/grpc/codes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type ReconcilerConfig struct {
	TrustDomainClient spireapi.TrustDomainClient
	K8sClient         client.Client

	// GCInterval how long to sit idle (i.e. untriggered) before doing
	// another reconcile.
	GCInterval time.Duration
}

func Reconciler(config ReconcilerConfig) reconciler.Reconciler {
	r := &federationRelationshipReconciler{
		config: config,
	}
	return reconciler.New("federation relationship", r.reconcile, config.GCInterval)
}

type federationRelationshipReconciler struct {
	config ReconcilerConfig
}

func (r *federationRelationshipReconciler) reconcile(ctx context.Context) {
	log := log.FromContext(ctx)

	currentRelationships, err := r.listFederationRelationships(ctx)
	if err != nil {
		log.Error(err, "Failed to list SPIRE federation relationships")
		return
	}

	clusterFederatedTrustDomains, err := r.listClusterFederatedTrustDomains(ctx)
	if err != nil {
		log.Error(err, "Failed to list ClusterFederatedTrustDomains")
		return
	}

	var toDelete []spireapi.FederationRelationship
	var toCreate []spireapi.FederationRelationship
	var toUpdate []spireapi.FederationRelationship

	for trustDomain, federationRelationship := range currentRelationships {
		if _, ok := clusterFederatedTrustDomains[trustDomain]; !ok {
			toDelete = append(toDelete, federationRelationship)
		}
	}
	for trustDomain, clusterFederatedTrustDomain := range clusterFederatedTrustDomains {
		currentRelationship, ok := currentRelationships[trustDomain]
		switch {
		case !ok:
			toCreate = append(toCreate, clusterFederatedTrustDomain.FederationRelationship)
		case !currentRelationship.Equal(clusterFederatedTrustDomain.FederationRelationship):
			toUpdate = append(toUpdate, clusterFederatedTrustDomain.FederationRelationship)
		}
	}

	if len(toDelete) > 0 {
		r.deleteFederationRelationships(ctx, toDelete)
	}
	if len(toCreate) > 0 {
		r.createFederationRelationships(ctx, toCreate)
	}
	if len(toUpdate) > 0 {
		r.updateFederationRelationships(ctx, toUpdate)
	}

	// TODO: Status updates
}

func (r *federationRelationshipReconciler) listFederationRelationships(ctx context.Context) (map[spiffeid.TrustDomain]spireapi.FederationRelationship, error) {
	federationRelationships, err := r.config.TrustDomainClient.ListFederationRelationships(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[spiffeid.TrustDomain]spireapi.FederationRelationship)
	for _, federationRelationship := range federationRelationships {
		out[federationRelationship.TrustDomain] = federationRelationship
	}
	return out, nil
}

func (r *federationRelationshipReconciler) listClusterFederatedTrustDomains(ctx context.Context) (map[spiffeid.TrustDomain]*clusterFederatedTrustDomainState, error) {
	log := log.FromContext(ctx)

	clusterFederatedTrustDomains, err := k8sapi.ListClusterFederatedTrustDomains(ctx, r.config.K8sClient)
	if err != nil {
		return nil, err
	}

	out := make(map[spiffeid.TrustDomain]*clusterFederatedTrustDomainState, len(clusterFederatedTrustDomains))
	for _, clusterFederatedTrustDomain := range clusterFederatedTrustDomains {
		log := log.WithValues(clusterFederatedTrustDomainLogKey, objectName(&clusterFederatedTrustDomain))

		federationRelationship, err := spirev1alpha1.ParseClusterFederatedTrustDomainSpec(&clusterFederatedTrustDomain.Spec)
		if err != nil {
			log.Error(err, "Ignoring invalid ClusterFederatedTrustDomain")
			continue
		}

		state := &clusterFederatedTrustDomainState{
			ClusterFederatedTrustDomain: clusterFederatedTrustDomain,
			FederationRelationship:      *federationRelationship,
		}

		if existing, ok := out[federationRelationship.TrustDomain]; ok {
			log.Error(nil, "Ignoring ClusterFederatedTrustDomain with conflicting trust domain",
				conflictWithKey, objectName(&existing.ClusterFederatedTrustDomain))
			continue
		}

		out[federationRelationship.TrustDomain] = state
	}
	return out, nil
}

func (r *federationRelationshipReconciler) createFederationRelationships(ctx context.Context, federationRelationships []spireapi.FederationRelationship) {
	log := log.FromContext(ctx)

	statuses, err := r.config.TrustDomainClient.CreateFederationRelationships(ctx, federationRelationships)
	if err != nil {
		log.Error(err, "Failed to create federation relationships")
		return
	}

	for i, status := range statuses {
		switch status.Code {
		case codes.OK:
			log.Info("Created federation relationship", federationRelationshipFields(federationRelationships[i])...)
		default:
			log.Error(status.Err(), "Failed to create federation relationship", federationRelationshipFields(federationRelationships[i])...)
		}
	}

}

func (r *federationRelationshipReconciler) updateFederationRelationships(ctx context.Context, federationRelationships []spireapi.FederationRelationship) {
	log := log.FromContext(ctx)

	statuses, err := r.config.TrustDomainClient.UpdateFederationRelationships(ctx, federationRelationships)
	if err != nil {
		log.Error(err, "Failed to update federation relationships")
		return
	}

	for i, status := range statuses {
		switch status.Code {
		case codes.OK:
			log.Info("Updated federation relationship", federationRelationshipFields(federationRelationships[i])...)
		default:
			log.Error(status.Err(), "Failed to update federation relationship", federationRelationshipFields(federationRelationships[i])...)
		}
	}
}

func (r *federationRelationshipReconciler) deleteFederationRelationships(ctx context.Context, federationRelationships []spireapi.FederationRelationship) {
	log := log.FromContext(ctx)

	statuses, err := r.config.TrustDomainClient.DeleteFederationRelationships(ctx, trustDomainIDsFromFederationRelationships(federationRelationships))
	if err != nil {
		log.Error(err, "Failed to delete federation relationships")
		return
	}

	for i, status := range statuses {
		switch status.Code {
		case codes.OK:
			log.Info("Deleted federation relationship", federationRelationshipFields(federationRelationships[i])...)
		default:
			log.Error(status.Err(), "Failed to delete federation relationship", federationRelationshipFields(federationRelationships[i])...)
		}
	}
}

func trustDomainIDsFromFederationRelationships(frs []spireapi.FederationRelationship) []spiffeid.TrustDomain {
	out := make([]spiffeid.TrustDomain, 0, len(frs))
	for _, fr := range frs {
		out = append(out, fr.TrustDomain)
	}
	return out
}

type clusterFederatedTrustDomainState struct {
	ClusterFederatedTrustDomain spirev1alpha1.ClusterFederatedTrustDomain
	FederationRelationship      spireapi.FederationRelationship
	NextStatus                  spirev1alpha1.ClusterFederatedTrustDomainStatus
}
