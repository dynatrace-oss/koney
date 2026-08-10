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

package annotations

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/dynatrace-oss/koney/api/v1alpha1"
	"github.com/dynatrace-oss/koney/internal/controller/constants"
)

// newAnnotatedPod returns a pod that carries a valid trap annotation for the given DeceptionPolicy.
func newAnnotatedPod(name string, crdName string) *corev1.Pod {
	changes := []v1alpha1.ChangeAnnotation{{
		DeceptionPolicyName: crdName,
		Traps: []v1alpha1.TrapAnnotation{{
			DeploymentStrategy: "containerExec",
			Containers:         []string{"container1"},
			FilesystemHoneytoken: v1alpha1.FilesystemHoneytokenAnnotation{
				FilePath:        testFilePath,
				FileContentHash: testFileHash,
			},
		}},
	}}

	changesJson, err := json.Marshal(changes)
	Expect(err).ShouldNot(HaveOccurred())

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   testNamespace,
			Annotations: map[string]string{constants.AnnotationKeyChanges: string(changesJson)},
		},
	}
}

// newPodWithBrokenAnnotation returns a pod whose trap annotation cannot be parsed.
func newPodWithBrokenAnnotation(name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   testNamespace,
			Annotations: map[string]string{constants.AnnotationKeyChanges: "-"},
		},
	}
}

var _ = Describe("GetAnnotatedResources", func() {
	var scheme *runtime.Scheme

	BeforeEach(func() {
		scheme = runtime.NewScheme()
		Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	})

	newClient := func(objects ...client.Object) client.Reader {
		return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	}

	Context("when all resources carry valid annotations", func() {
		It("should return the annotated resources", func() {
			r := newClient(newAnnotatedPod(testPodName, testCrdName))

			resources, err := GetAnnotatedResources(r, context.Background(), testCrdName)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(resources).Should(HaveLen(1))
			Expect(resources[0].GetName()).Should(Equal(testPodName))
		})
	})

	Context("when another resource carries an annotation that cannot be parsed", func() {
		It("should skip that resource and still return the others", func() {
			r := newClient(
				newPodWithBrokenAnnotation("broken-pod"),
				newAnnotatedPod(testPodName, testCrdName),
			)

			resources, err := GetAnnotatedResources(r, context.Background(), testCrdName)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(resources).Should(HaveLen(1))
			Expect(resources[0].GetName()).Should(Equal(testPodName))
		})
	})

	Context("when the only resource carries an annotation that cannot be parsed", func() {
		It("should return no resources and no error, so that clean-up can finish", func() {
			r := newClient(newPodWithBrokenAnnotation("broken-pod"))

			resources, err := GetAnnotatedResources(r, context.Background(), testCrdName)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(resources).Should(BeEmpty())
		})
	})
})
