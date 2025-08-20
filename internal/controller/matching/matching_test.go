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

package matching

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/dynatrace-oss/koney/api/v1alpha1"
	"github.com/dynatrace-oss/koney/internal/controller/utils"
)

// interceptCreationTimestamp can be added to the fake client with
// WithInterceptorFuncs() to override the creation timestamp of the pods in the
// list result. Typically, the fake client returns pods with a creation
// timestamp of the time when the fake client was created. This function allows
// to set the creation timestamp of the pods to the value of the passed original pods.
func interceptCreationTimestamp(originalPods []*corev1.Pod) interceptor.Funcs {
	return interceptor.Funcs{
		List: func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			result := client.List(ctx, list, opts...)
			for i := range list.(*corev1.PodList).Items {
				for _, pod := range originalPods {
					if list.(*corev1.PodList).Items[i].Name == pod.Name {
						list.(*corev1.PodList).Items[i].SetCreationTimestamp(pod.CreationTimestamp)
					}
				}
			}
			return result
		},
	}
}

var _ = Describe("GetDeployableObjectsWithContainers", func() {
	var fakeClient client.Client
	var ctx context.Context

	const (
		KoneyNamespace    = "koney"
		MatchLabelKey     = "koney/match"
		MatchLabelValue   = "yes"
		NoMatchLabelKey   = "koney/no-match"
		NoMatchLabelValue = "no"
	)

	var (
		testTrapForPods        v1alpha1.Trap
		testTrapForDeployments v1alpha1.Trap

		podNotOk_Old_Run_CtrsReady_Ctr1RunAndReady                    corev1.Pod
		podOk_Old_Run_CtrsReady_Ctr1RunAndReady                       corev1.Pod
		podOk_New_Run_CtrsReady_Ctr1RunAndReady                       corev1.Pod
		podOk_Old_Run_CtrsNotReady_Ctr1RunAndNotReady                 corev1.Pod
		podOk_Old_Run_CtrsNotReady_Ctr1NoRunAndNotReady               corev1.Pod
		podOk_Old_Run_CtrsNotReady_Ctr1RunAndReady_Ctr2RunAndNotReady corev1.Pod
		podOk_Old_NoRun_NoPodCond_Ctr1NoRunAndNotReady                corev1.Pod

		allTestPods []*corev1.Pod

		deplNotOk_Old_Available appsv1.Deployment
		deplOk_Old_Available    appsv1.Deployment
		deplOk_Old_NotAvailable appsv1.Deployment
	)

	BeforeEach(func() {
		ctx = context.TODO()

		testTrapForPods = v1alpha1.Trap{
			DecoyDeployment: v1alpha1.DecoyDeployment{
				Strategy: "containerExec",
			},
			MatchResources: v1alpha1.MatchResources{
				Any: []v1alpha1.ResourceFilter{
					{
						ResourceDescription: v1alpha1.ResourceDescription{
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{MatchLabelKey: MatchLabelValue},
							},
						},
					},
				},
			},
		}

		testTrapForDeployments = v1alpha1.Trap{
			DecoyDeployment: v1alpha1.DecoyDeployment{
				Strategy: "volumeMount",
			},
			MatchResources: v1alpha1.MatchResources{
				Any: []v1alpha1.ResourceFilter{
					{
						ResourceDescription: v1alpha1.ResourceDescription{
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{MatchLabelKey: MatchLabelValue},
							},
						},
					},
				},
			},
		}

		createdBefore := metav1.NewTime(time.Now().Add(-10 * time.Minute))
		createdAfter := metav1.NewTime(time.Now().Add(10 * time.Minute))

		// pod NOT matching, created before, running, containers ready, one container running and ready
		podNotOk_Old_Run_CtrsReady_Ctr1RunAndReady = corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "podNotOk_Old_Run_CtrsReady_Ctr1RunAndReady",
				Namespace:         KoneyNamespace,
				CreationTimestamp: createdBefore,
				Labels: map[string]string{
					NoMatchLabelKey: NoMatchLabelValue,
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{Type: corev1.ContainersReady, Status: corev1.ConditionTrue},
				},
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name: "foo", Ready: true,
						State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
					},
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "foo"}},
			},
		}

		// pod matching, created before, running, containers ready, one container running and ready
		podOk_Old_Run_CtrsReady_Ctr1RunAndReady = corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "podOk_Old_Run_CtrsReady_Ctr1RunAndReady",
				Namespace:         KoneyNamespace,
				CreationTimestamp: createdBefore,
				Labels: map[string]string{
					MatchLabelKey: MatchLabelValue,
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{Type: corev1.ContainersReady, Status: corev1.ConditionTrue},
				},
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name: "foo", Ready: true,
						State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
					},
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "foo"}},
			},
		}

		// pod matching, created after, running, containers ready, one container running and ready
		podOk_New_Run_CtrsReady_Ctr1RunAndReady = corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "podOk_New_Run_CtrsReady_Ctr1RunAndReady",
				Namespace:         KoneyNamespace,
				CreationTimestamp: createdAfter,
				Labels: map[string]string{
					MatchLabelKey: MatchLabelValue,
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{Type: corev1.ContainersReady, Status: corev1.ConditionTrue},
				},
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name: "foo", Ready: true,
						State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
					},
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "foo"}},
			},
		}

		// pod matching, created before, running, containers NOT ready, one container running and NOT ready
		podOk_Old_Run_CtrsNotReady_Ctr1RunAndNotReady = corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "podOk_Old_Run_CtrsNotReady_Ctr1RunAndNotReady",
				Namespace:         KoneyNamespace,
				CreationTimestamp: createdBefore,
				Labels: map[string]string{
					MatchLabelKey: MatchLabelValue,
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{Type: corev1.ContainersReady, Status: corev1.ConditionFalse},
				},
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name: "foo", Ready: false,
						State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
					},
				},
			},
			Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "foo"}}},
		}

		// pod matching, created before, running, containers NOT ready, one container NOT running and NOT ready
		podOk_Old_Run_CtrsNotReady_Ctr1NoRunAndNotReady = corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "podOk_Old_Run_CtrsNotReady_Ctr1NoRunAndNotReady",
				Namespace:         KoneyNamespace,
				CreationTimestamp: createdBefore,
				Labels: map[string]string{
					MatchLabelKey: MatchLabelValue,
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{Type: corev1.ContainersReady, Status: corev1.ConditionFalse},
				},
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name: "foo", Ready: false,
						State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{}},
					},
				},
			},
			Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "foo"}}},
		}

		// pod matching, created before, running, containers NOT ready, one container running and ready, one container running and NOT ready
		podOk_Old_Run_CtrsNotReady_Ctr1RunAndReady_Ctr2RunAndNotReady = corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "podOk_Old_Run_CtrsNotReady_Ctr1RunAndReady_Ctr2RunAndNotReady",
				Namespace:         KoneyNamespace,
				CreationTimestamp: createdBefore,
				Labels: map[string]string{
					MatchLabelKey: MatchLabelValue,
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{Type: corev1.ContainersReady, Status: corev1.ConditionFalse},
				},
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name: "foo", Ready: true,
						State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
					},
					{
						Name: "bar", Ready: false,
						State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{}},
					},
				},
			},
			Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "foo"}, {Name: "bar"}}},
		}

		// pod matching, created before, NOT running, NO pod conditions, one container NOT running and NOT ready
		podOk_Old_NoRun_NoPodCond_Ctr1NoRunAndNotReady = corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "podOk_Old_NoRun_NoPodCond_Ctr1NoRunAndNotReady",
				Namespace:         KoneyNamespace,
				CreationTimestamp: createdBefore,
				Labels: map[string]string{
					MatchLabelKey: MatchLabelValue,
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name: "foo", Ready: false,
						State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{}},
					},
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "foo"}},
			},
		}

		// deployment NOT matching, created before, available
		deplNotOk_Old_Available = appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "deplNotOk_Old_Available",
				Namespace:         KoneyNamespace,
				CreationTimestamp: createdBefore,
				Labels: map[string]string{
					NoMatchLabelKey: NoMatchLabelValue,
				},
			},
			Status: appsv1.DeploymentStatus{
				Conditions: []appsv1.DeploymentCondition{
					{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue},
				},
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "foo"}}},
				},
			},
		}

		// deployment matching, created before, available
		deplOk_Old_Available = appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "deplOk_Old_Available",
				Namespace:         KoneyNamespace,
				CreationTimestamp: createdBefore,
				Labels: map[string]string{
					MatchLabelKey: MatchLabelValue,
				},
			},
			Status: appsv1.DeploymentStatus{
				Conditions: []appsv1.DeploymentCondition{
					{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue},
				},
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "foo"}}},
				},
			},
		}

		// deployment matching, created before, not available (no status conditions)
		deplOk_Old_NotAvailable = appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "deplOk_Old_NotAvailable",
				Namespace:         KoneyNamespace,
				CreationTimestamp: createdBefore,
				Labels: map[string]string{
					MatchLabelKey: MatchLabelValue,
				},
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "foo"}}},
				},
			},
		}

		allTestPods = []*corev1.Pod{
			&podNotOk_Old_Run_CtrsReady_Ctr1RunAndReady,
			&podOk_Old_Run_CtrsReady_Ctr1RunAndReady,
			&podOk_New_Run_CtrsReady_Ctr1RunAndReady,
			&podOk_Old_Run_CtrsNotReady_Ctr1RunAndNotReady,
			&podOk_Old_Run_CtrsNotReady_Ctr1NoRunAndNotReady,
			&podOk_Old_Run_CtrsNotReady_Ctr1RunAndReady_Ctr2RunAndNotReady,
			&podOk_Old_NoRun_NoPodCond_Ctr1NoRunAndNotReady,
		}
	})

	Context("With one not-matching pod", func() {
		It("should match no pod", func() {
			podList := corev1.PodList{
				Items: []corev1.Pod{
					podNotOk_Old_Run_CtrsReady_Ctr1RunAndReady,
				},
			}

			fakeClient = fake.NewClientBuilder().WithLists(&podList).Build()

			matchResult, err := GetDeployableObjectsWithContainers(fakeClient, ctx, testTrapForPods, nil)
			Expect(err).ToNot(HaveOccurred())

			Expect(matchResult.DeployableObjects).To(BeEmpty())
			Expect(matchResult.AtLeastOneObjectWasMatched).To(BeFalse())
			Expect(matchResult.AllDeployableObjectsWereReady).To(BeFalse()) // technically undefined
		})

	})

	Context("With one matching, but not-ready pod", func() {
		It("should match no pod", func() {
			podList := corev1.PodList{
				Items: []corev1.Pod{
					podOk_Old_Run_CtrsNotReady_Ctr1RunAndNotReady,
				},
			}

			fakeClient = fake.NewClientBuilder().WithLists(&podList).Build()

			matchResult, err := GetDeployableObjectsWithContainers(fakeClient, ctx, testTrapForPods, nil)
			Expect(err).ToNot(HaveOccurred())

			Expect(matchResult.DeployableObjects).To(BeEmpty())
			Expect(matchResult.AtLeastOneObjectWasMatched).To(BeTrue())
			Expect(matchResult.AllDeployableObjectsWereReady).To(BeFalse())
		})

	})

	Context("With one matching, and ready pod", func() {
		It("should match the only pod", func() {
			podList := corev1.PodList{
				Items: []corev1.Pod{
					podOk_Old_Run_CtrsReady_Ctr1RunAndReady,
				},
			}

			fakeClient = fake.NewClientBuilder().WithLists(&podList).Build()

			matchResult, err := GetDeployableObjectsWithContainers(fakeClient, ctx, testTrapForPods, nil)
			Expect(err).ToNot(HaveOccurred())

			Expect(matchResult.DeployableObjects).To(HaveLen(1))
			obj := getObjectFromMap(podOk_Old_Run_CtrsReady_Ctr1RunAndReady.Name, matchResult.DeployableObjects)
			Expect(obj).NotTo(BeNil())
			Expect(obj.GetName()).To(Equal(podOk_Old_Run_CtrsReady_Ctr1RunAndReady.Name))
			Expect(matchResult.DeployableObjects[obj]).To(HaveLen(1))
			Expect(matchResult.DeployableObjects[obj][0]).To(Equal(podOk_Old_Run_CtrsReady_Ctr1RunAndReady.Spec.Containers[0].Name))

			Expect(matchResult.AtLeastOneObjectWasMatched).To(BeTrue())
			Expect(matchResult.AllDeployableObjectsWereReady).To(BeTrue())
		})

	})

	Context("With two matching, and ready pods, one old, one new", func() {
		It("should match only the pod that is newer than the policy", func() {
			podList := corev1.PodList{
				Items: []corev1.Pod{
					podOk_Old_Run_CtrsReady_Ctr1RunAndReady,
					podOk_New_Run_CtrsReady_Ctr1RunAndReady,
				},
			}

			fakeClient = fake.NewClientBuilder().WithLists(&podList).WithInterceptorFuncs(interceptCreationTimestamp(allTestPods)).Build()
			deceptionPolicyCreatedAt := metav1.Now()

			matchResult, err := GetDeployableObjectsWithContainers(fakeClient, ctx, testTrapForPods, &deceptionPolicyCreatedAt)
			Expect(err).ToNot(HaveOccurred())

			Expect(matchResult.DeployableObjects).To(HaveLen(1))
			obj := getObjectFromMap(podOk_New_Run_CtrsReady_Ctr1RunAndReady.Name, matchResult.DeployableObjects)
			Expect(obj).NotTo(BeNil())
			Expect(obj.GetName()).To(Equal(podOk_New_Run_CtrsReady_Ctr1RunAndReady.Name))
			Expect(matchResult.DeployableObjects[obj]).To(HaveLen(1))
			Expect(matchResult.DeployableObjects[obj][0]).To(Equal(podOk_New_Run_CtrsReady_Ctr1RunAndReady.Spec.Containers[0].Name))

			Expect(matchResult.AtLeastOneObjectWasMatched).To(BeTrue())
			Expect(matchResult.AllDeployableObjectsWereReady).To(BeTrue())
		})

		It("should match both pods when the policy is older than both pods", func() {
			podList := corev1.PodList{
				Items: []corev1.Pod{
					podOk_Old_Run_CtrsReady_Ctr1RunAndReady,
					podOk_New_Run_CtrsReady_Ctr1RunAndReady,
				},
			}

			fakeClient = fake.NewClientBuilder().WithLists(&podList).WithInterceptorFuncs(interceptCreationTimestamp(allTestPods)).Build()
			deceptionPolicyCreatedAt := metav1.NewTime(time.Now().Add(-6 * time.Hour))

			matchResult, err := GetDeployableObjectsWithContainers(fakeClient, ctx, testTrapForPods, &deceptionPolicyCreatedAt)
			Expect(err).ToNot(HaveOccurred())

			Expect(matchResult.DeployableObjects).To(HaveLen(2))

			obj1 := getObjectFromMap(podOk_Old_Run_CtrsReady_Ctr1RunAndReady.Name, matchResult.DeployableObjects)
			Expect(obj1).NotTo(BeNil())
			Expect(obj1.GetName()).To(Equal(podOk_Old_Run_CtrsReady_Ctr1RunAndReady.Name))
			Expect(matchResult.DeployableObjects[obj1]).To(HaveLen(1))
			Expect(matchResult.DeployableObjects[obj1][0]).To(Equal(podOk_Old_Run_CtrsReady_Ctr1RunAndReady.Spec.Containers[0].Name))

			obj2 := getObjectFromMap(podOk_New_Run_CtrsReady_Ctr1RunAndReady.Name, matchResult.DeployableObjects)
			Expect(obj2).NotTo(BeNil())
			Expect(obj2.GetName()).To(Equal(podOk_New_Run_CtrsReady_Ctr1RunAndReady.Name))
			Expect(matchResult.DeployableObjects[obj2]).To(HaveLen(1))
			Expect(matchResult.DeployableObjects[obj2][0]).To(Equal(podOk_New_Run_CtrsReady_Ctr1RunAndReady.Spec.Containers[0].Name))

			Expect(matchResult.AtLeastOneObjectWasMatched).To(BeTrue())
			Expect(matchResult.AllDeployableObjectsWereReady).To(BeTrue())
		})

	})

	Context("With four matching, one ready, two not-ready pods, one not running yet", func() {
		It("should only match the ready pod", func() {
			podList := corev1.PodList{
				Items: []corev1.Pod{
					podOk_Old_Run_CtrsReady_Ctr1RunAndReady,
					podOk_Old_Run_CtrsNotReady_Ctr1RunAndNotReady,
					podOk_Old_Run_CtrsNotReady_Ctr1NoRunAndNotReady,
					podOk_Old_NoRun_NoPodCond_Ctr1NoRunAndNotReady,
				},
			}

			fakeClient = fake.NewClientBuilder().WithLists(&podList).Build()

			matchResult, err := GetDeployableObjectsWithContainers(fakeClient, ctx, testTrapForPods, nil)
			Expect(err).ToNot(HaveOccurred())

			Expect(matchResult.DeployableObjects).To(HaveLen(1))
			obj := getObjectFromMap(podOk_Old_Run_CtrsReady_Ctr1RunAndReady.Name, matchResult.DeployableObjects)
			Expect(obj).NotTo(BeNil())
			Expect(obj.GetName()).To(Equal(podOk_Old_Run_CtrsReady_Ctr1RunAndReady.Name))
			Expect(matchResult.DeployableObjects[obj]).To(HaveLen(1))
			Expect(matchResult.DeployableObjects[obj][0]).To(Equal(podOk_Old_Run_CtrsReady_Ctr1RunAndReady.Spec.Containers[0].Name))

			Expect(matchResult.AtLeastOneObjectWasMatched).To(BeTrue())
			Expect(matchResult.AllDeployableObjectsWereReady).To(BeFalse())
		})

	})

	Context("With four matching, one ready, two not-ready pods, one partially ready", func() {
		It("should only match the two ready pods and the one ready container", func() {
			podList := corev1.PodList{
				Items: []corev1.Pod{
					podOk_Old_Run_CtrsReady_Ctr1RunAndReady,
					podOk_Old_Run_CtrsNotReady_Ctr1RunAndNotReady,
					podOk_Old_Run_CtrsNotReady_Ctr1NoRunAndNotReady,
					podOk_Old_Run_CtrsNotReady_Ctr1RunAndReady_Ctr2RunAndNotReady,
				},
			}

			fakeClient = fake.NewClientBuilder().WithLists(&podList).Build()

			matchResult, err := GetDeployableObjectsWithContainers(fakeClient, ctx, testTrapForPods, nil)
			Expect(err).ToNot(HaveOccurred())

			Expect(matchResult.DeployableObjects).To(HaveLen(2))

			obj1 := getObjectFromMap(podOk_Old_Run_CtrsReady_Ctr1RunAndReady.Name, matchResult.DeployableObjects)
			Expect(obj1).NotTo(BeNil())
			Expect(obj1.GetName()).To(Equal(podOk_Old_Run_CtrsReady_Ctr1RunAndReady.Name))
			Expect(matchResult.DeployableObjects[obj1]).To(HaveLen(1))
			Expect(matchResult.DeployableObjects[obj1][0]).To(Equal(podOk_Old_Run_CtrsReady_Ctr1RunAndReady.Spec.Containers[0].Name))

			obj2 := getObjectFromMap(podOk_Old_Run_CtrsNotReady_Ctr1RunAndReady_Ctr2RunAndNotReady.Name, matchResult.DeployableObjects)
			Expect(obj2).NotTo(BeNil())
			Expect(obj2.GetName()).To(Equal(podOk_Old_Run_CtrsNotReady_Ctr1RunAndReady_Ctr2RunAndNotReady.Name))
			Expect(matchResult.DeployableObjects[obj2]).To(HaveLen(1))
			Expect(matchResult.DeployableObjects[obj2][0]).To(Equal(podOk_Old_Run_CtrsNotReady_Ctr1RunAndReady_Ctr2RunAndNotReady.Spec.Containers[0].Name))

			Expect(matchResult.AtLeastOneObjectWasMatched).To(BeTrue())
			Expect(matchResult.AllDeployableObjectsWereReady).To(BeFalse())
		})

	})

	Context("With one not-matching deployment", func() {
		It("should match no deployment", func() {
			deploymentList := appsv1.DeploymentList{
				Items: []appsv1.Deployment{
					deplNotOk_Old_Available,
				},
			}

			fakeClient = fake.NewClientBuilder().WithLists(&deploymentList).Build()

			matchResult, err := GetDeployableObjectsWithContainers(fakeClient, ctx, testTrapForDeployments, nil)
			Expect(err).ToNot(HaveOccurred())

			Expect(matchResult.DeployableObjects).To(BeEmpty())
			Expect(matchResult.AtLeastOneObjectWasMatched).To(BeFalse())
			Expect(matchResult.AllDeployableObjectsWereReady).To(BeFalse()) // technically undefined
		})

	})

	Context("With one matching, and ready deployment", func() {
		It("should match the only deployment", func() {
			deploymentList := appsv1.DeploymentList{
				Items: []appsv1.Deployment{
					deplOk_Old_Available,
				},
			}

			fakeClient = fake.NewClientBuilder().WithLists(&deploymentList).Build()

			matchResult, err := GetDeployableObjectsWithContainers(fakeClient, ctx, testTrapForDeployments, nil)
			Expect(err).ToNot(HaveOccurred())

			Expect(matchResult.DeployableObjects).To(HaveLen(1))
			obj := getObjectFromMap(deplOk_Old_Available.Name, matchResult.DeployableObjects)
			Expect(obj).NotTo(BeNil())
			Expect(obj.GetName()).To(Equal(deplOk_Old_Available.Name))
			Expect(matchResult.DeployableObjects[obj]).To(HaveLen(1))
			Expect(matchResult.DeployableObjects[obj][0]).To(Equal(deplOk_Old_Available.Spec.Template.Spec.Containers[0].Name))

			Expect(matchResult.AtLeastOneObjectWasMatched).To(BeTrue())
			Expect(matchResult.AllDeployableObjectsWereReady).To(BeTrue())
		})

	})

	Context("With two matching, one ready, one not-ready deployment", func() {
		It("should only match the ready pod", func() {
			deploymentList := appsv1.DeploymentList{
				Items: []appsv1.Deployment{
					deplOk_Old_Available,
					deplOk_Old_NotAvailable,
				},
			}

			fakeClient = fake.NewClientBuilder().WithLists(&deploymentList).Build()

			matchResult, err := GetDeployableObjectsWithContainers(fakeClient, ctx, testTrapForDeployments, nil)
			Expect(err).ToNot(HaveOccurred())

			Expect(matchResult.DeployableObjects).To(HaveLen(1))
			obj := getObjectFromMap(deplOk_Old_Available.Name, matchResult.DeployableObjects)
			Expect(obj).NotTo(BeNil())
			Expect(obj.GetName()).To(Equal(deplOk_Old_Available.Name))
			Expect(matchResult.DeployableObjects[obj]).To(HaveLen(1))
			Expect(matchResult.DeployableObjects[obj][0]).To(Equal(deplOk_Old_Available.Spec.Template.Spec.Containers[0].Name))

			Expect(matchResult.AtLeastOneObjectWasMatched).To(BeTrue())
			Expect(matchResult.AllDeployableObjectsWereReady).To(BeFalse())
		})

	})
})

var _ = Describe("getMatchingPodsWithContainers", func() {
	var client client.Client
	var ctx context.Context

	const (
		KoneyNamespace   = "koney"
		OtherNamespace   = "other"
		KoneyLabelAKey   = "koney/A"
		KoneyLabelAValue = "a"
		KoneyLabelBKey   = "koney/B"
		KoneyLabelBValue = "b"
		KoneyLabelCKey   = "koney/C"
		KoneyLabelCValue = "c"
	)

	var (
		koneyPodWithLabelA    corev1.Pod
		koneyPodWithLabelB    corev1.Pod
		koneyPodWithLabelC    corev1.Pod
		koneyPodWithLabelAB   corev1.Pod
		koneyPodWithLabelABC  corev1.Pod
		koneyPodWithoutLabels corev1.Pod
		otherPodWithLabelC    corev1.Pod
		otherPodWithoutLabels corev1.Pod
	)

	BeforeEach(func() {
		ctx = context.TODO()

		readyStatus := corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.ContainersReady, Status: corev1.ConditionTrue},
			},
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "foo", Ready: true,
					State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
				},
			},
		}

		koneyPodWithLabelA = corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "koney-pod-with-label-a",
				Namespace: KoneyNamespace,
				Labels: map[string]string{
					KoneyLabelAKey: KoneyLabelAValue,
				},
			},
			Status: readyStatus,
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "foo"}},
			},
		}

		koneyPodWithLabelB = corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "koney-pod-with-label-b",
				Namespace: KoneyNamespace,
				Labels: map[string]string{
					KoneyLabelBKey: KoneyLabelBValue,
				},
			},
			Status: readyStatus,
			Spec:   corev1.PodSpec{Containers: []corev1.Container{{Name: "foo"}}},
		}

		koneyPodWithLabelC = corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "koney-pod-with-label-c",
				Namespace: KoneyNamespace,
				Labels: map[string]string{
					KoneyLabelCKey: KoneyLabelCValue,
				},
			},
			Status: readyStatus,
			Spec:   corev1.PodSpec{Containers: []corev1.Container{{Name: "foo"}}},
		}

		koneyPodWithLabelAB = corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "koney-pod-with-label-ab",
				Namespace: KoneyNamespace,
				Labels: map[string]string{
					KoneyLabelAKey: KoneyLabelAValue,
					KoneyLabelBKey: KoneyLabelBValue,
				},
			},
			Status: readyStatus,
			Spec:   corev1.PodSpec{Containers: []corev1.Container{{Name: "foo"}}},
		}

		koneyPodWithLabelABC = corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "koney-pod-with-label-abc",
				Namespace: KoneyNamespace,
				Labels: map[string]string{
					KoneyLabelAKey: KoneyLabelAValue,
					KoneyLabelBKey: KoneyLabelBValue,
					KoneyLabelCKey: KoneyLabelCValue,
				},
			},
			Status: readyStatus,
			Spec:   corev1.PodSpec{Containers: []corev1.Container{{Name: "foo"}}},
		}

		koneyPodWithoutLabels = corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "koney-pod-without-labels",
				Namespace: KoneyNamespace,
			},
			Status: readyStatus,
			Spec:   corev1.PodSpec{Containers: []corev1.Container{{Name: "foo"}}},
		}

		otherPodWithLabelC = corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-pod-with-label-c",
				Namespace: OtherNamespace,
				Labels: map[string]string{
					KoneyLabelCKey: KoneyLabelCValue,
				},
			},
			Status: readyStatus,
			Spec:   corev1.PodSpec{Containers: []corev1.Container{{Name: "foo"}}},
		}

		otherPodWithoutLabels = corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-pod-without-labels",
				Namespace: OtherNamespace,
			},
			Status: readyStatus,
			Spec:   corev1.PodSpec{Containers: []corev1.Container{{Name: "foo"}}},
		}
	})

	Context("With a full list of pods", func() {
		var podList corev1.PodList

		BeforeEach(func() {
			podList = corev1.PodList{
				Items: []corev1.Pod{
					koneyPodWithLabelA,
					koneyPodWithLabelB,
					koneyPodWithLabelC,
					koneyPodWithLabelAB,
					koneyPodWithLabelABC,
					koneyPodWithoutLabels,
					otherPodWithLabelC,
					otherPodWithoutLabels,
				},
			}

			client = fake.NewClientBuilder().WithLists(&podList).Build()
		})

		It("should match nothing with empty match object", func() {
			match := v1alpha1.MatchResources{}

			matchingPodsWithContainers, err := getMatchingPodsWithContainers(client, ctx, match)
			Expect(err).ToNot(HaveOccurred())

			Expect(matchingPodsWithContainers).To(BeEmpty())
		})

		It("should match nothing with empty any object", func() {
			match := v1alpha1.MatchResources{
				Any: []v1alpha1.ResourceFilter{},
			}

			matchingPodsWithContainers, err := getMatchingPodsWithContainers(client, ctx, match)
			Expect(err).ToNot(HaveOccurred())

			Expect(matchingPodsWithContainers).To(BeEmpty())
		})

		It("should match nothing with neither selector nor namespaces set", func() {
			match := v1alpha1.MatchResources{
				Any: []v1alpha1.ResourceFilter{
					{
						ResourceDescription: v1alpha1.ResourceDescription{},
					},
				},
			}

			matchingPodsWithContainers, err := getMatchingPodsWithContainers(client, ctx, match)
			Expect(err).ToNot(HaveOccurred())

			Expect(matchingPodsWithContainers).To(BeEmpty())
		})

		It("should match nothing with empty selector and namespaces set", func() {
			match := v1alpha1.MatchResources{
				Any: []v1alpha1.ResourceFilter{
					{
						ResourceDescription: v1alpha1.ResourceDescription{
							Selector:   &metav1.LabelSelector{},
							Namespaces: []string{},
						},
					},
				},
			}

			matchingPodsWithContainers, err := getMatchingPodsWithContainers(client, ctx, match)
			Expect(err).ToNot(HaveOccurred())

			Expect(matchingPodsWithContainers).To(BeEmpty())
		})

		It("should match nothing with empty selector.matchLabels and namespaces set", func() {
			match := v1alpha1.MatchResources{
				Any: []v1alpha1.ResourceFilter{
					{
						ResourceDescription: v1alpha1.ResourceDescription{
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{},
							},
							Namespaces: []string{},
						},
					},
				},
			}

			matchingPodsWithContainers, err := getMatchingPodsWithContainers(client, ctx, match)
			Expect(err).ToNot(HaveOccurred())

			Expect(matchingPodsWithContainers).To(BeEmpty())
		})

		It("should match a single label", func() {
			match := v1alpha1.MatchResources{
				Any: []v1alpha1.ResourceFilter{
					{
						ResourceDescription: v1alpha1.ResourceDescription{
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									KoneyLabelAKey: KoneyLabelAValue,
								},
							},
						},
					},
				},
			}

			matchingPodsWithContainers, err := getMatchingPodsWithContainers(client, ctx, match)
			Expect(err).ToNot(HaveOccurred())

			matchingPods := utils.GetMapKeys(matchingPodsWithContainers)

			matchingPodNames := extractObjectNames(matchingPods)
			Expect(matchingPodNames).To(HaveLen(3))
			Expect(matchingPodNames).To(ConsistOf(
				koneyPodWithLabelA.Name, koneyPodWithLabelAB.Name, koneyPodWithLabelABC.Name))
		})

		It("should match a single label (even with empty namespaces set)", func() {
			match := v1alpha1.MatchResources{
				Any: []v1alpha1.ResourceFilter{
					{
						ResourceDescription: v1alpha1.ResourceDescription{
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									KoneyLabelAKey: KoneyLabelAValue,
								},
							},
							Namespaces: []string{},
						},
					},
				},
			}

			matchingPodsWithContainers, err := getMatchingPodsWithContainers(client, ctx, match)
			Expect(err).ToNot(HaveOccurred())

			matchingPods := utils.GetMapKeys(matchingPodsWithContainers)

			matchingPodNames := extractObjectNames(matchingPods)
			Expect(matchingPodNames).To(HaveLen(3))
			Expect(matchingPodNames).To(ConsistOf(
				koneyPodWithLabelA.Name, koneyPodWithLabelAB.Name, koneyPodWithLabelABC.Name))
		})

		It("should not match a non-existent label", func() {
			match := v1alpha1.MatchResources{
				Any: []v1alpha1.ResourceFilter{
					{
						ResourceDescription: v1alpha1.ResourceDescription{
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									KoneyLabelAKey: KoneyLabelBValue,
								},
							},
						},
					},
				},
			}

			matchingPodsWithContainers, err := getMatchingPodsWithContainers(client, ctx, match)
			Expect(err).ToNot(HaveOccurred())
			Expect(matchingPodsWithContainers).To(BeEmpty())
		})

		It("should match multiple labels (expect logical and)", func() {
			match := v1alpha1.MatchResources{
				Any: []v1alpha1.ResourceFilter{
					{
						ResourceDescription: v1alpha1.ResourceDescription{
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									KoneyLabelAKey: KoneyLabelAValue,
									KoneyLabelBKey: KoneyLabelBValue,
								},
							},
						},
					},
				},
			}

			matchingPodsWithContainers, err := getMatchingPodsWithContainers(client, ctx, match)
			Expect(err).ToNot(HaveOccurred())

			matchingPods := utils.GetMapKeys(matchingPodsWithContainers)

			matchingPodNames := extractObjectNames(matchingPods)
			Expect(matchingPodNames).To(HaveLen(2))
			Expect(matchingPodNames).To(ConsistOf(
				koneyPodWithLabelAB.Name, koneyPodWithLabelABC.Name))
		})

		It("should match multiple labels in separate filters (expect logical or)", func() {
			match := v1alpha1.MatchResources{
				Any: []v1alpha1.ResourceFilter{
					{
						ResourceDescription: v1alpha1.ResourceDescription{
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									KoneyLabelAKey: KoneyLabelAValue,
								},
							},
						},
					},
					{
						ResourceDescription: v1alpha1.ResourceDescription{
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									KoneyLabelBKey: KoneyLabelBValue,
								},
							},
						},
					},
				},
			}

			matchingPodsWithContainers, err := getMatchingPodsWithContainers(client, ctx, match)
			Expect(err).ToNot(HaveOccurred())

			matchingPods := utils.GetMapKeys(matchingPodsWithContainers)

			matchingPodNames := extractObjectNames(matchingPods)
			Expect(matchingPodNames).To(HaveLen(4))
			Expect(matchingPodNames).To(ConsistOf(
				koneyPodWithLabelA.Name, koneyPodWithLabelB.Name,
				koneyPodWithLabelAB.Name, koneyPodWithLabelABC.Name))
		})

		It("should match single namespace", func() {
			match := v1alpha1.MatchResources{
				Any: []v1alpha1.ResourceFilter{
					{
						ResourceDescription: v1alpha1.ResourceDescription{
							Namespaces: []string{KoneyNamespace},
						},
					},
				},
			}

			matchingPodsWithContainers, err := getMatchingPodsWithContainers(client, ctx, match)
			Expect(err).ToNot(HaveOccurred())

			matchingPods := utils.GetMapKeys(matchingPodsWithContainers)

			matchingPodNames := extractObjectNames(matchingPods)
			Expect(matchingPodNames).To(HaveLen(6))
			Expect(matchingPodNames).To(ConsistOf(
				koneyPodWithLabelA.Name, koneyPodWithLabelB.Name, koneyPodWithLabelC.Name,
				koneyPodWithLabelAB.Name, koneyPodWithLabelABC.Name, koneyPodWithoutLabels.Name))
		})

		It("should match single namespace (even with empty matchLabels set)", func() {
			match := v1alpha1.MatchResources{
				Any: []v1alpha1.ResourceFilter{
					{
						ResourceDescription: v1alpha1.ResourceDescription{
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{},
							},
							Namespaces: []string{KoneyNamespace},
						},
					},
				},
			}

			matchingPodsWithContainers, err := getMatchingPodsWithContainers(client, ctx, match)
			Expect(err).ToNot(HaveOccurred())

			matchingPods := utils.GetMapKeys(matchingPodsWithContainers)

			matchingPodNames := extractObjectNames(matchingPods)
			Expect(matchingPodNames).To(HaveLen(6))
			Expect(matchingPodNames).To(ConsistOf(
				koneyPodWithLabelA.Name, koneyPodWithLabelB.Name, koneyPodWithLabelC.Name,
				koneyPodWithLabelAB.Name, koneyPodWithLabelABC.Name, koneyPodWithoutLabels.Name))
		})

		It("should match multiple namespaces (expect logical or)", func() {
			match := v1alpha1.MatchResources{
				Any: []v1alpha1.ResourceFilter{
					{
						ResourceDescription: v1alpha1.ResourceDescription{
							Namespaces: []string{KoneyNamespace, OtherNamespace},
						},
					},
				},
			}

			matchingPodsWithContainers, err := getMatchingPodsWithContainers(client, ctx, match)
			Expect(err).ToNot(HaveOccurred())

			matchingPods := utils.GetMapKeys(matchingPodsWithContainers)

			matchingPodNames := extractObjectNames(matchingPods)
			Expect(matchingPodNames).To(HaveLen(8))
			Expect(matchingPodNames).To(ConsistOf(
				koneyPodWithLabelA.Name, koneyPodWithLabelB.Name, koneyPodWithLabelC.Name,
				koneyPodWithLabelAB.Name, koneyPodWithLabelABC.Name, koneyPodWithoutLabels.Name,
				otherPodWithLabelC.Name, otherPodWithoutLabels.Name))
		})

		It("should match multiple namespaces in separate filters (expect logical or)", func() {
			match := v1alpha1.MatchResources{
				Any: []v1alpha1.ResourceFilter{
					{
						ResourceDescription: v1alpha1.ResourceDescription{
							Namespaces: []string{KoneyNamespace},
						},
					},
					{
						ResourceDescription: v1alpha1.ResourceDescription{
							Namespaces: []string{OtherNamespace},
						},
					},
				},
			}

			matchingPodsWithContainers, err := getMatchingPodsWithContainers(client, ctx, match)
			Expect(err).ToNot(HaveOccurred())

			matchingPods := utils.GetMapKeys(matchingPodsWithContainers)

			matchingPodNames := extractObjectNames(matchingPods)
			Expect(matchingPodNames).To(HaveLen(8))
			Expect(matchingPodNames).To(ConsistOf(
				koneyPodWithLabelA.Name, koneyPodWithLabelB.Name, koneyPodWithLabelC.Name,
				koneyPodWithLabelAB.Name, koneyPodWithLabelABC.Name, koneyPodWithoutLabels.Name,
				otherPodWithLabelC.Name, otherPodWithoutLabels.Name))
		})

		It("should match single label and namespace (expect logical and)", func() {
			match := v1alpha1.MatchResources{
				Any: []v1alpha1.ResourceFilter{
					{
						ResourceDescription: v1alpha1.ResourceDescription{
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									KoneyLabelCKey: KoneyLabelCValue,
								},
							},
							Namespaces: []string{OtherNamespace},
						},
					},
				},
			}

			matchingPodsWithContainers, err := getMatchingPodsWithContainers(client, ctx, match)
			Expect(err).ToNot(HaveOccurred())

			matchingPods := utils.GetMapKeys(matchingPodsWithContainers)

			matchingPodNames := extractObjectNames(matchingPods)
			Expect(matchingPodNames).To(HaveLen(1))
			Expect(matchingPodNames).To(ConsistOf(otherPodWithLabelC.Name))
		})

		It("should match single label and namespace in separate filters (expect logical or)", func() {
			match := v1alpha1.MatchResources{
				Any: []v1alpha1.ResourceFilter{
					{
						ResourceDescription: v1alpha1.ResourceDescription{
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									KoneyLabelCKey: KoneyLabelCValue,
								},
							},
						},
					},
					{
						ResourceDescription: v1alpha1.ResourceDescription{
							Namespaces: []string{OtherNamespace},
						},
					},
				},
			}

			matchingPodsWithContainers, err := getMatchingPodsWithContainers(client, ctx, match)
			Expect(err).ToNot(HaveOccurred())

			matchingPods := utils.GetMapKeys(matchingPodsWithContainers)

			matchingPodNames := extractObjectNames(matchingPods)
			Expect(matchingPodNames).To(HaveLen(4))
			Expect(matchingPodNames).To(ConsistOf(
				koneyPodWithLabelC.Name, koneyPodWithLabelABC.Name,
				otherPodWithLabelC.Name, otherPodWithoutLabels.Name))
		})
	})
})

var _ = Describe("selectContainers", func() {
	var pod corev1.Pod

	Context("With a pod that has four containers", func() {
		BeforeEach(func() {
			pod = corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "foo"},
						{Name: "bar"},
						{Name: "baz"},
						{Name: "quz"},
					},
				},
			}
		})

		It("should select a single container", func() {
			selection, err := selectContainers(&pod, "foo")
			Expect(err).ToNot(HaveOccurred())
			Expect(selection).To(ConsistOf("foo"))
		})

		It("should select no containers", func() {
			selection, err := selectContainers(&pod, "non-existing")
			Expect(err).ToNot(HaveOccurred())
			Expect(selection).To(BeEmpty())
		})

		It("regex should select a single container", func() {
			selection, err := selectContainers(&pod, "regex:foo")
			Expect(err).ToNot(HaveOccurred())
			Expect(selection).To(ConsistOf("foo"))
		})

		It("regex should select no containers", func() {
			selection, err := selectContainers(&pod, "regex:non-existing")
			Expect(err).ToNot(HaveOccurred())
			Expect(selection).To(BeEmpty())
		})

		It("regex should select all containers", func() {
			selection, err := selectContainers(&pod, "regex:.*")
			Expect(err).ToNot(HaveOccurred())
			Expect(selection).To(ConsistOf("foo", "bar", "baz", "quz"))
		})

		It("regex should select containers starting with some string", func() {
			selection, err := selectContainers(&pod, "regex:^b")
			Expect(err).ToNot(HaveOccurred())
			Expect(selection).To(ConsistOf("bar", "baz"))
		})

		It("regex should select containers ending with some string", func() {
			selection, err := selectContainers(&pod, "regex:z$")
			Expect(err).ToNot(HaveOccurred())
			Expect(selection).To(ConsistOf("baz", "quz"))
		})

		It("regex should select containers containing some string", func() {
			selection, err := selectContainers(&pod, "regex:a")
			Expect(err).ToNot(HaveOccurred())
			Expect(selection).To(ConsistOf("bar", "baz"))
		})

		It("glob should select all containers", func() {
			selection, err := selectContainers(&pod, "glob:*")
			Expect(err).ToNot(HaveOccurred())
			Expect(selection).To(ConsistOf("foo", "bar", "baz", "quz"))
		})

		It("glob should select containers starting with some string", func() {
			selection, err := selectContainers(&pod, "glob:b*")
			Expect(err).ToNot(HaveOccurred())
			Expect(selection).To(ConsistOf("bar", "baz"))
		})

		It("glob should select containers ending with some string", func() {
			selection, err := selectContainers(&pod, "glob:*z")
			Expect(err).ToNot(HaveOccurred())
			Expect(selection).To(ConsistOf("baz", "quz"))
		})

		It("glob should select containers containing some string", func() {
			selection, err := selectContainers(&pod, "glob:*a*")
			Expect(err).ToNot(HaveOccurred())
			Expect(selection).To(ConsistOf("bar", "baz"))
		})
	})
})
