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

package webhooktoken

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
)

const testNamespace = "koney-system"

func newSecrets(objects ...runtime.Object) corev1client.SecretInterface {
	return fake.NewClientset(objects...).CoreV1().Secrets(testNamespace)
}

func readToken(secrets corev1client.SecretInterface) string {
	secret, err := secrets.Get(context.Background(), secretName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	return string(secret.Data[secretKey])
}

var _ = Describe("InitWebhookToken", func() {
	It("creates a secret with a random token", func() {
		secrets := newSecrets()

		Expect(initWebhookToken(context.Background(), secrets, testNamespace)).To(Succeed())
		token := readToken(secrets)
		Expect(token).NotTo(BeEmpty())

		otherSecrets := newSecrets()
		Expect(initWebhookToken(context.Background(), otherSecrets, testNamespace)).To(Succeed())
		Expect(readToken(otherSecrets)).NotTo(Equal(token))
	})

	It("keeps the token of an earlier installation", func() {
		secrets := newSecrets(&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: testNamespace},
			Data:       map[string][]byte{secretKey: []byte("token-of-an-earlier-installation")},
		})

		Expect(initWebhookToken(context.Background(), secrets, testNamespace)).To(Succeed())
		Expect(readToken(secrets)).To(Equal("token-of-an-earlier-installation"))
	})

	It("fills in a secret that carries no token", func() {
		secrets := newSecrets(&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:        secretName,
				Namespace:   testNamespace,
				Annotations: map[string]string{"managed-by-someone": "else"},
			},
			Data: map[string][]byte{"other": []byte("keep me")},
		})

		Expect(initWebhookToken(context.Background(), secrets, testNamespace)).To(Succeed())
		Expect(readToken(secrets)).NotTo(BeEmpty())

		secret, err := secrets.Get(context.Background(), secretName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(secret.Data).To(HaveKeyWithValue("other", []byte("keep me")))
		Expect(secret.Annotations).To(HaveKeyWithValue("managed-by-someone", "else"))
	})
})
