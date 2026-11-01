/*
Copyright 2026 ferrix

SPDX-License-Identifier: MIT
*/

package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	infrav1 "github.com/ferrix/capturing/api/v1beta1"
	"github.com/ferrix/capturing/pkg/bmc"
)

const (
	reasonBMCReachable   = "BMCReachable"
	reasonBMCUnreachable = "BMCUnreachable"
	reasonMissingSecret  = "MissingCredentials"
	reasonInvalidNode    = "InvalidNode"
	reasonSlotConflict   = "SlotConflict"
	reasonPowerFailed    = "PowerFailed"
	reasonFlashFailed    = "FlashFailed"
	reasonDeleting       = "Deleting"
	reasonPaused         = "Paused"
	reasonProvisioned    = "Provisioned"
)

func isAdopted(obj metav1.Object) bool {
	if obj == nil {
		return false
	}
	return obj.GetAnnotations()[infrav1.AdoptedAnnotation] != ""
}

func isPaused(cluster *clusterv1.Cluster, obj metav1.Object) bool {
	if obj != nil && obj.GetAnnotations()[clusterv1.PausedAnnotation] != "" {
		return true
	}
	if cluster == nil {
		return false
	}
	if cluster.GetAnnotations()[clusterv1.PausedAnnotation] != "" {
		return true
	}
	return ptr.Deref(cluster.Spec.Paused, false)
}

func setReady(conditions *[]metav1.Condition, generation int64, ready bool, reason, message string) {
	status := metav1.ConditionFalse
	if ready {
		status = metav1.ConditionTrue
	}
	meta.SetStatusCondition(conditions, metav1.Condition{
		Type:               clusterv1.ReadyCondition,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: generation,
	})
}

func ownerMachineName(obj metav1.Object) string {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.Kind == "Machine" {
			return ref.Name
		}
	}
	return ""
}

func bmcClientFor(ctx context.Context, c client.Client, cluster *infrav1.TuringPiCluster, opts ...bmc.Option) (*bmc.HTTPClient, error) {
	if cluster.Spec.BMC.URL == "" {
		return nil, fmt.Errorf("bmc url is required")
	}
	if cluster.Spec.BMC.CredentialsRef.Name == "" {
		return nil, fmt.Errorf("bmc credentialsRef is required")
	}
	secret := &corev1.Secret{}
	key := types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Spec.BMC.CredentialsRef.Name}
	if err := c.Get(ctx, key, secret); err != nil {
		return nil, fmt.Errorf("credentials secret: %w", err)
	}
	user := string(secret.Data["username"])
	pass := string(secret.Data["password"])
	if user == "" || pass == "" {
		return nil, fmt.Errorf("credentials secret %q must contain username and password", secret.Name)
	}
	if cluster.Spec.BMC.InsecureSkipVerify {
		opts = append(opts, bmc.WithInsecureSkipVerify())
	}
	return bmc.New(cluster.Spec.BMC.URL, user, pass, opts...), nil
}

func patch(ctx context.Context, c client.Client, original, current client.Object) error {
	// Patch status and spec from independent copies. Each Patch call reloads the
	// object, so sharing `current` would drop the other half of the mutation.
	statusObj := current.DeepCopyObject().(client.Object)
	if err := c.Status().Patch(ctx, statusObj, client.MergeFrom(original)); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if err := c.Patch(ctx, current, client.MergeFrom(original)); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func defaultBMCOptions(opts []bmc.Option) []bmc.Option {
	if len(opts) == 0 {
		return []bmc.Option{bmc.WithPollInterval(time.Second)}
	}
	return opts
}
