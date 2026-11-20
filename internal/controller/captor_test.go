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

	ciliumiov1alpha1 "github.com/cilium/tetragon/pkg/k8s/apis/cilium.io/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dynatrace-oss/koney/api/v1alpha1"
	"github.com/dynatrace-oss/koney/internal/controller/constants"
	"github.com/dynatrace-oss/koney/internal/controller/traps/filesystoken"
	"github.com/dynatrace-oss/koney/internal/controller/utils"
)

var _ = Describe("Tetragon captor deployment", func() {
	ctx := context.Background()

	It("should take over a TracingPolicy that already exists under the same name", func() {
		trap := v1alpha1.Trap{
			FilesystemHoneytoken: v1alpha1.FilesystemHoneytoken{FilePath: "/run/secrets/koney/existing_policy"},
			CaptorDeployment:     v1alpha1.CaptorDeployment{Strategy: "tetragon"},
			MatchResources: v1alpha1.MatchResources{Any: []v1alpha1.ResourceFilter{{
				ResourceDescription: v1alpha1.ResourceDescription{
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "existing-policy"}},
				},
			}}},
		}

		deceptionPolicy := &v1alpha1.DeceptionPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: "existing-tracingpolicy", Namespace: utils.GetKoneyNamespace()},
			Spec:       v1alpha1.DeceptionPolicySpec{Traps: []v1alpha1.Trap{trap}},
		}
		Expect(k8sClient.Create(ctx, deceptionPolicy)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, deceptionPolicy)
		deceptionPolicy.SetGroupVersionKind(v1alpha1.GroupVersion.WithKind("DeceptionPolicy"))

		name, err := filesystoken.GenerateTetragonTracingPolicyName(trap)
		Expect(err).NotTo(HaveOccurred())

		By("creating a TracingPolicy with the same name, e.g., left over from an earlier Koney run")
		existing := &ciliumiov1alpha1.TracingPolicy{ObjectMeta: metav1.ObjectMeta{Name: name}}
		Expect(k8sClient.Create(ctx, existing)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, existing)

		reconciler := &filesystoken.FilesystemHoneytokenReconciler{Client: k8sClient}
		Expect(reconciler.DeployCaptor(ctx, deceptionPolicy, trap).Errors).NotTo(HaveOccurred())

		By("checking that Koney's spec replaced the existing one")
		applied := &ciliumiov1alpha1.TracingPolicy{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name}, applied)).To(Succeed())
		Expect(applied.Spec.KProbes).NotTo(BeEmpty())
		Expect(applied.Labels).To(HaveKeyWithValue(constants.LabelKeyDeceptionPolicyRef, deceptionPolicy.Name))
	})
})
