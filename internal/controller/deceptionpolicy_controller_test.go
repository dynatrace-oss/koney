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

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/dynatrace-oss/koney/api/v1alpha1"
	"github.com/dynatrace-oss/koney/internal/controller/utils"
)

var _ = Describe("DeceptionPolicy Controller", func() {

	Context("When reconciling a resource", func() {
		const resourceName = "test-deceptionpolicy"
		var resourceNamespace = utils.GetKoneyNamespace()
		namespacedName := types.NamespacedName{Name: resourceName, Namespace: resourceNamespace}
		deceptionPolicy := &v1alpha1.DeceptionPolicy{}

		ctx := context.Background()

		BeforeEach(func() {
			By("Creating the custom resource for the Kind DeceptionPolicy")
			err := k8sClient.Get(ctx, namespacedName, deceptionPolicy)
			if err != nil && errors.IsNotFound(err) {
				resource := &v1alpha1.DeceptionPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: resourceNamespace,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &v1alpha1.DeceptionPolicy{}
			err := k8sClient.Get(ctx, namespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Clean-up the DeceptionPolicy instance")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should successfully reconcile the resource", func() {
			controllerReconciler := &DeceptionPolicyReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling the DeceptionPolicy for the first time")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Reconciling the DeceptionPolicy for the second time")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, namespacedName, deceptionPolicy)
			Expect(err).NotTo(HaveOccurred())

			By("Checking the status of the DeceptionPolicy")
			condition := deceptionPolicy.Status.GetCondition(ResourceFoundType)
			Expect(condition.Type).To(Equal(ResourceFoundType))
			Expect(condition.Reason).To(Equal(ResourceFoundReason_Found))
			Expect(condition.Message).To(Equal(ResourceFoundMessage_Found))
			Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		})
	})

})
