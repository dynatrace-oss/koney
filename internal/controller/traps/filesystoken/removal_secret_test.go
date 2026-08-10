// Copyright (c) 2025 Dynatrace LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package filesystoken

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/dynatrace-oss/koney/api/v1alpha1"
)

const (
	secretTestNamespace  = "test-namespace"
	secretTestFilePath   = "/run/secrets/koney/service_token"
	secretTestSecretName = "koney-secret-test"
)

// newSecretTestReconciler returns a reconciler backed by a fake client that knows the given objects.
func newSecretTestReconciler(objects ...client.Object) *FilesystemHoneytokenReconciler {
	scheme := runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	Expect(v1alpha1.AddToScheme(scheme)).To(Succeed())

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	return &FilesystemHoneytokenReconciler{Client: fakeClient, Scheme: scheme}
}

// newDeploymentMountingHoneytoken returns a deployment that mounts the honeytoken secret in one container.
func newDeploymentMountingHoneytoken(name string) *appsv1.Deployment {
	volumeName := generateVolumeName(secretTestFilePath)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: secretTestNamespace},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:         "app",
						VolumeMounts: []corev1.VolumeMount{{Name: volumeName, MountPath: secretTestFilePath}},
					}},
					Volumes: []corev1.Volume{{
						Name:         volumeName,
						VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: secretTestSecretName}},
					}},
				},
			},
		},
	}
}

func newHoneytokenSecret(name string) *corev1.Secret {
	return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: secretTestNamespace}}
}

func newVolumeMountTrapAnnotation() v1alpha1.TrapAnnotation {
	return v1alpha1.TrapAnnotation{
		DeploymentStrategy:   "volumeMount",
		Containers:           []string{"app"},
		FilesystemHoneytoken: v1alpha1.FilesystemHoneytokenAnnotation{FilePath: secretTestFilePath},
	}
}

// honeytokenSecretExists reports whether the secret is still present in the fake cluster.
func honeytokenSecretExists(r *FilesystemHoneytokenReconciler, name string) bool {
	secret := corev1.Secret{}
	err := r.Get(context.Background(), client.ObjectKey{Namespace: secretTestNamespace, Name: name}, &secret)
	if apierrors.IsNotFound(err) {
		return false
	}

	Expect(err).ShouldNot(HaveOccurred())
	return true
}

var _ = Describe("RemoveDecoy with the volumeMount strategy", func() {
	Context("when no other deployment mounts the honeytoken secret", func() {
		It("should delete the secret", func() {
			deployment := newDeploymentMountingHoneytoken("web")
			r := newSecretTestReconciler(deployment, newHoneytokenSecret(secretTestSecretName))

			Expect(r.RemoveDecoy(context.Background(), "policy-a", newVolumeMountTrapAnnotation(), deployment)).To(Succeed())
			Expect(honeytokenSecretExists(r, secretTestSecretName)).Should(BeFalse())
		})
	})

	Context("when another deployment still mounts the same honeytoken secret", func() {
		It("should keep the secret", func() {
			deployment := newDeploymentMountingHoneytoken("web")
			other := newDeploymentMountingHoneytoken("api")
			r := newSecretTestReconciler(deployment, other, newHoneytokenSecret(secretTestSecretName))

			Expect(r.RemoveDecoy(context.Background(), "policy-a", newVolumeMountTrapAnnotation(), deployment)).To(Succeed())
			Expect(honeytokenSecretExists(r, secretTestSecretName)).Should(BeTrue())
		})
	})

	Context("when the deployment cannot be updated", func() {
		It("should keep the secret that the deployment still mounts", func() {
			// The deployment is not in the cluster, so the update fails and the volume is never removed
			deployment := newDeploymentMountingHoneytoken("web")
			r := newSecretTestReconciler(newHoneytokenSecret(secretTestSecretName))

			Expect(r.RemoveDecoy(context.Background(), "policy-a", newVolumeMountTrapAnnotation(), deployment)).ShouldNot(Succeed())
			Expect(honeytokenSecretExists(r, secretTestSecretName)).Should(BeTrue())
		})
	})
})
