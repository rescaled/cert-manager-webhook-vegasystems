package main

import (
	"context"
	"encoding/json"
	"errors"
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

const (
	// defaultTTL matches the lowest TTL the upstream VegaSystems API accepts
	// (300 seconds / 5 minutes). Lower values are rejected with a validation
	// error at create time.
	defaultTTL = 300
	opTimeout  = 30 * time.Second
)

var GroupName = os.Getenv("GROUP_NAME")

func main() {
	if GroupName == "" {
		panic("GROUP_NAME must be specified")
	}
	cmd.RunWebhookServer(GroupName, &solver{})
}

type solver struct {
	kube kubernetes.Interface
	http *http.Client
	ctx  context.Context
}

func (s *solver) opContext() (context.Context, context.CancelFunc) {
	base := s.ctx
	if base == nil {
		base = context.Background()
	}
	return context.WithTimeout(base, opTimeout)
}

type providerConfig struct {
	CustomerID       int                      `json:"customerId"`
	APIUserSecretRef cmmeta.SecretKeySelector `json:"apiUserSecretRef"`
	APIKeySecretRef  cmmeta.SecretKeySelector `json:"apiKeySecretRef"`
	TTL              int                      `json:"ttl,omitempty"`
	BaseURL          string                   `json:"baseURL,omitempty"`
}

func (s *solver) Name() string { return "vegasystems" }

func (s *solver) Initialize(kubeClientConfig *rest.Config, stopCh <-chan struct{}) error {
	cl, err := kubernetes.NewForConfig(kubeClientConfig)
	if err != nil {
		return fmt.Errorf("build kubernetes client: %w", err)
	}
	s.kube = cl
	s.http = &http.Client{Timeout: 30 * time.Second}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-stopCh
		cancel()
	}()
	s.ctx = ctx
	return nil
}

func (s *solver) Present(ch *v1alpha1.ChallengeRequest) error {
	ctx, cancel := s.opContext()
	defer cancel()
	cfg, client, zone, name, err := s.prepare(ctx, ch)
	if err != nil {
		return err
	}
	domID, err := client.FindDomainID(ctx, zone, cfg.CustomerID)
	if err != nil {
		return err
	}
	existing, err := client.ListRecords(ctx, domID)
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
	return client.CreateRecord(ctx, domID, name, "TXT", ttl, ch.Key)
}

func (s *solver) CleanUp(ch *v1alpha1.ChallengeRequest) error {
	ctx, cancel := s.opContext()
	defer cancel()
	cfg, client, zone, name, err := s.prepare(ctx, ch)
	if err != nil {
		return err
	}
	domID, err := client.FindDomainID(ctx, zone, cfg.CustomerID)
	if err != nil {
		return err
	}
	records, err := client.ListRecords(ctx, domID)
	if err != nil {
		return err
	}
	var errs []error
	for _, r := range records {
		if r.Type == "TXT" && r.Name == name && r.Value == ch.Key {
			if err := client.DeleteRecord(ctx, domID, r.RecordID()); err != nil {
				errs = append(errs, fmt.Errorf("delete record %s: %w", r.RecordID(), err))
			}
		}
	}
	return errors.Join(errs...)
}

func (s *solver) prepare(ctx context.Context, ch *v1alpha1.ChallengeRequest) (providerConfig, *vegasystems.Client, string, string, error) {
	cfg, err := loadConfig(ch.Config)
	if err != nil {
		return cfg, nil, "", "", err
	}
	if cfg.CustomerID <= 0 {
		return cfg, nil, "", "", fmt.Errorf("customerId must be a positive integer")
	}

	user, err := s.resolveSecret(ctx, ch.ResourceNamespace, cfg.APIUserSecretRef, "apiUserSecretRef")
	if err != nil {
		return cfg, nil, "", "", err
	}
	key, err := s.resolveSecret(ctx, ch.ResourceNamespace, cfg.APIKeySecretRef, "apiKeySecretRef")
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

func (s *solver) resolveSecret(ctx context.Context, namespace string, sel cmmeta.SecretKeySelector, fieldName string) (string, error) {
	if sel.Name == "" {
		return "", fmt.Errorf("%s.name is required", fieldName)
	}
	if sel.Key == "" {
		return "", fmt.Errorf("%s.key is required", fieldName)
	}
	if s.kube == nil {
		return "", fmt.Errorf("kubernetes client not initialised")
	}
	secret, err := s.kube.CoreV1().Secrets(namespace).Get(ctx, sel.Name, metav1.GetOptions{})
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
