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

package forwarder

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/dynatrace-oss/koney/api/v1alpha1"
)

const sinkNamespace = "koney-system"

func alertSinkResource(name, secretName string) *v1alpha1.DeceptionAlertSink {
	return &v1alpha1.DeceptionAlertSink{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: sinkNamespace},
		Spec: v1alpha1.DeceptionAlertSinkSpec{
			Dynatrace: v1alpha1.DynatraceSinkSpec{SecretName: secretName, Severity: "HIGH"},
		},
	}
}

func sinkSecret(name, apiURL string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: sinkNamespace},
		Data:       map[string][]byte{"apiUrl": []byte(apiURL), "apiToken": []byte("dt0c01.token")},
	}
}

var _ = Describe("Alert sinks", func() {
	It("should resolve the sinks with their credentials", func() {
		client := fake.NewClientBuilder().WithScheme(testScheme).WithObjects(
			alertSinkResource("sink", "sink-secret"),
			sinkSecret("sink-secret", "https://abc.dynatrace.com"),
		).Build()
		forwarder := New(Options{Client: client, Namespace: sinkNamespace})

		sinks := forwarder.readAlertSinks(context.Background())

		Expect(sinks).To(HaveLen(1))
		Expect(sinks[0].name).To(Equal("sink"))
		Expect(sinks[0].dynatrace.apiURL).To(Equal("https://abc.dynatrace.com"))
		Expect(sinks[0].dynatrace.apiToken).To(Equal("dt0c01.token"))
		Expect(sinks[0].dynatrace.severity).To(Equal("HIGH"))
	})

	It("should skip the sinks whose secret is missing or incomplete", func() {
		client := fake.NewClientBuilder().WithScheme(testScheme).WithObjects(
			alertSinkResource("without-secret", "missing-secret"),
			alertSinkResource("without-url", "incomplete-secret"),
			sinkSecret("incomplete-secret", ""),
		).Build()
		forwarder := New(Options{Client: client, Namespace: sinkNamespace})

		Expect(forwarder.readAlertSinks(context.Background())).To(BeEmpty())
	})

	It("should send an alert to a Dynatrace sink", func() {
		var payload map[string]any
		var authorization string

		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			defer GinkgoRecover()

			Expect(request.URL.Path).To(Equal(dynatraceIngestPath))
			authorization = request.Header.Get("Authorization")
			Expect(json.NewDecoder(request.Body).Decode(&payload)).To(Succeed())

			writer.WriteHeader(http.StatusAccepted)
		}))
		defer server.Close()

		client := fake.NewClientBuilder().WithScheme(testScheme).WithObjects(
			alertSinkResource("sink", "sink-secret"),
			sinkSecret("sink-secret", server.URL),
		).Build()
		clientset := k8sfake.NewClientset(
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: clusterUIDNamespace, UID: "cluster-uid"}},
		)
		forwarder := New(Options{Client: client, Clientset: clientset, Namespace: sinkNamespace})

		sinks := forwarder.readAlertSinks(context.Background())
		Expect(forwarder.sendAlert(context.Background(), sampleAlert, sinks[0])).To(Succeed())

		Expect(authorization).To(Equal("Api-Token dt0c01.token"))
		Expect(payload).To(HaveKeyWithValue("event.provider", "Koney"))
		Expect(payload).To(HaveKeyWithValue("k8s.cluster.uid", "cluster-uid"))
	})

	It("should report a sink that rejects an alert", func() {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			http.Error(writer, "nope", http.StatusForbidden)
		}))
		defer server.Close()

		client := fake.NewClientBuilder().WithScheme(testScheme).WithObjects(
			alertSinkResource("sink", "sink-secret"),
			sinkSecret("sink-secret", server.URL),
		).Build()
		forwarder := New(Options{Client: client, Clientset: k8sfake.NewClientset(), Namespace: sinkNamespace})

		sinks := forwarder.readAlertSinks(context.Background())
		Expect(forwarder.sendAlert(context.Background(), sampleAlert, sinks[0])).To(MatchError(ContainSubstring("403")))
	})
})
