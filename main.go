package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	extapi "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	"github.com/cert-manager/cert-manager/pkg/acme/webhook/cmd"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"

	"cert-manager-webhook-vegasystems/vegasystems"
)

const defaultTTL = 120

var GroupName = os.Getenv("GROUP_NAME")

func main() {
	if GroupName == "" {
		panic("GROUP_NAME must be specified")
	}
	cmd.RunWebhookServer(GroupName, &solver{})
}

type solver struct {
	kube *kubernetes.Clientset
	http *http.Client
}

type providerConfig struct {
	CustomerID       int                      `json:"customerId"`
	APIUserSecretRef cmmeta.SecretKeySelector `json:"apiUserSecretRef"`
	APIKeySecretRef  cmmeta.SecretKeySelector `json:"apiKeySecretRef"`
	TTL              int                      `json:"ttl,omitempty"`
	BaseURL          string                   `json:"baseURL,omitempty"`
}

func (s *solver) Name() string { return "vegasystems" }

func (s *solver) Initialize(kubeClientConfig *rest.Config, _ <-chan struct{}) error {
	cl, err := kubernetes.NewForConfig(kubeClientConfig)
	if err != nil {
		return fmt.Errorf("build kubernetes client: %w", err)
	}
	s.kube = cl
	s.http = &http.Client{Timeout: 30 * time.Second}
	return nil
}

func (s *solver) Present(ch *v1alpha1.ChallengeRequest) error {
	cfg, client, zone, name, err := s.prepare(ch)
	if err != nil {
		return err
	}
	domID, err := client.FindDomainID(zone, cfg.CustomerID)
	if err != nil {
		return err
	}
	existing, err := client.ListRecords(domID)
	if err != nil {
		return err
	}
	for _, r := range existing {
		if r.Type == "TXT" && r.Name == name && r.Value == ch.Key {
			return nil
		}
	}
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = defaultTTL
	}
	return client.CreateRecord(domID, name, "TXT", ttl, ch.Key)
}

func (s *solver) CleanUp(ch *v1alpha1.ChallengeRequest) error {
	cfg, client, zone, name, err := s.prepare(ch)
	if err != nil {
		return err
	}
	domID, err := client.FindDomainID(zone, cfg.CustomerID)
	if err != nil {
		return err
	}
	records, err := client.ListRecords(domID)
	if err != nil {
		return err
	}
	for _, r := range records {
		if r.Type == "TXT" && r.Name == name && r.Value == ch.Key {
			if err := client.DeleteRecord(domID, r.RecordID()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *solver) prepare(ch *v1alpha1.ChallengeRequest) (providerConfig, *vegasystems.Client, string, string, error) {
	cfg, err := loadConfig(ch.Config)
	if err != nil {
		return cfg, nil, "", "", err
	}
	if cfg.CustomerID == 0 {
		return cfg, nil, "", "", fmt.Errorf("customerId is required in webhook config")
	}

	user, err := s.resolveSecret(ch.ResourceNamespace, cfg.APIUserSecretRef, "apiUserSecretRef")
	if err != nil {
		return cfg, nil, "", "", err
	}
	key, err := s.resolveSecret(ch.ResourceNamespace, cfg.APIKeySecretRef, "apiKeySecretRef")
	if err != nil {
		return cfg, nil, "", "", err
	}

	client := vegasystems.New(user, key)
	client.HTTP = s.http
	if cfg.BaseURL != "" {
		client.BaseURL = cfg.BaseURL
	}

	zone := strings.TrimSuffix(ch.ResolvedZone, ".")
	fqdn := strings.TrimSuffix(ch.ResolvedFQDN, ".")
	name := strings.TrimSuffix(strings.TrimSuffix(fqdn, zone), ".")

	return cfg, client, zone, name, nil
}

func (s *solver) resolveSecret(namespace string, sel cmmeta.SecretKeySelector, fieldName string) (string, error) {
	if sel.Name == "" {
		return "", fmt.Errorf("%s.name is required", fieldName)
	}
	if sel.Key == "" {
		return "", fmt.Errorf("%s.key is required", fieldName)
	}
	if s.kube == nil {
		return "", fmt.Errorf("kubernetes client not initialised")
	}
	secret, err := s.kube.CoreV1().Secrets(namespace).Get(context.Background(), sel.Name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get secret %s/%s: %w", namespace, sel.Name, err)
	}
	val, ok := secret.Data[sel.Key]
	if !ok {
		return "", fmt.Errorf("key %q not found in secret %s/%s", sel.Key, namespace, sel.Name)
	}
	return strings.TrimSpace(string(val)), nil
}

func loadConfig(cfgJSON *extapi.JSON) (providerConfig, error) {
	cfg := providerConfig{}
	if cfgJSON == nil {
		return cfg, nil
	}
	if err := json.Unmarshal(cfgJSON.Raw, &cfg); err != nil {
		return cfg, fmt.Errorf("decode solver config: %w", err)
	}
	return cfg, nil
}
