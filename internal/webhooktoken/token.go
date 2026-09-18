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
	"crypto/rand"
	"encoding/base64"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/dynatrace-oss/koney/internal/controller/utils"
)

const (
	// secretName is the name of the secret that holds the token of the alert forwarder webhooks.
	secretName = "koney-alert-forwarder-token"

	// secretKey is the key of the token within the secret.
	secretKey = "token"

	tokenLength = 32
)

var initLog = ctrl.Log.WithName("webhook-token")

// InitWebhookToken creates the secret that holds the token of the alert forwarder webhooks.
// The token of an earlier installation is kept, so that the URLs of existing traps stay valid.
// It runs in an init container, before the controller and the alert forwarder read the token.
func InitWebhookToken(ctx context.Context) error {
	config, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("unable to load the cluster configuration: %w", err)
	}

	client, err := corev1client.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("unable to build a Kubernetes client: %w", err)
	}

	namespace := utils.GetKoneyNamespace()

	return initWebhookToken(ctx, client.Secrets(namespace), namespace)
}

func initWebhookToken(ctx context.Context, secrets corev1client.SecretInterface, namespace string) error {
	existing, err := secrets.Get(ctx, secretName, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("unable to read the secret %q: %w", secretName, err)
	}
	secretIsMissing := apierrors.IsNotFound(err)

	if !secretIsMissing && len(existing.Data[secretKey]) > 0 {
		initLog.Info("Webhook token found", "secret", secretName)
		return nil
	}

	token, err := generateToken()
	if err != nil {
		return err
	}

	if secretIsMissing {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: namespace,
				Labels: map[string]string{
					"app.kubernetes.io/name":       "koney",
					"app.kubernetes.io/managed-by": "koney-controller",
				},
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{secretKey: []byte(token)},
		}

		if _, err := secrets.Create(ctx, secret, metav1.CreateOptions{}); err != nil {
			if apierrors.IsAlreadyExists(err) {
				initLog.Info("Webhook token created by someone else", "secret", secretName)
				return nil
			}
			return fmt.Errorf("unable to create the secret %q: %w", secretName, err)
		}

		initLog.Info("Webhook token created", "secret", secretName)
		return nil
	}

	// keep everything else that the secret carries
	if existing.Data == nil {
		existing.Data = map[string][]byte{}
	}
	existing.Data[secretKey] = []byte(token)

	if _, err := secrets.Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
		if apierrors.IsConflict(err) {
			initLog.Info("Webhook token written by someone else", "secret", secretName)
			return nil
		}
		return fmt.Errorf("unable to update the secret %q: %w", secretName, err)
	}

	initLog.Info("Webhook token restored", "secret", secretName)
	return nil
}

func generateToken() (string, error) {
	buffer := make([]byte, tokenLength)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("unable to generate a webhook token: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
