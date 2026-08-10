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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/dynatrace-oss/koney/api/v1alpha1"
)

// newRemovalReconciler returns a reconciler backed by a fake client that knows the given objects.
func newRemovalReconciler(objects ...client.Object) *FilesystemHoneytokenReconciler {
	scheme := runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	Expect(v1alpha1.AddToScheme(scheme)).To(Succeed())

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	return &FilesystemHoneytokenReconciler{Client: fakeClient, Scheme: scheme}
}

var _ = Describe("RemoveDecoy", func() {
	Context("when the annotation says containerExec but the resource is a deployment", func() {
		It("should return an error instead of panicking", func() {
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "test-namespace"},
			}
			trap := v1alpha1.TrapAnnotation{DeploymentStrategy: "containerExec", Containers: []string{"app"}}

			r := newRemovalReconciler(deployment)
			var err error
			Expect(func() {
				err = r.RemoveDecoy(context.Background(), "policy-a", trap, deployment)
			}).ShouldNot(Panic())
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("is not a pod"))
		})
	})

	Context("when the annotation says volumeMount but the resource is a pod", func() {
		It("should return an error instead of panicking", func() {
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web-0", Namespace: "test-namespace"}}
			trap := v1alpha1.TrapAnnotation{DeploymentStrategy: "volumeMount", Containers: []string{"app"}}

			r := newRemovalReconciler(pod)
			var err error
			Expect(func() {
				err = r.RemoveDecoy(context.Background(), "policy-a", trap, pod)
			}).ShouldNot(Panic())
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("is not a deployment"))
		})
	})
})
