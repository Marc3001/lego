// Package scalewaydedibox implements a DNS provider for solving the DNS-01 challenge using Scaleway dedibox API.
package scalewaydedibox

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-acme/lego/v4/challenge/dns01"
	"github.com/go-acme/lego/v4/platform/config/env"
	"github.com/go-acme/lego/v4/providers/dns/scalewaydedibox/internal"
)

// Environment variables names.
const (
	envNamespace = "SCALEWAYDEDIBOX_"

	EnvAPIToken = envNamespace + "API_TOKEN"

	EnvTTL                 = envNamespace + "TTL"
	EnvPropagationTimeout  = envNamespace + "PROPAGATION_TIMEOUT"
	EnvPollingInterval     = envNamespace + "POLLING_INTERVAL"
	EnvHTTPTimeout         = envNamespace + "HTTP_TIMEOUT"
	EnvTempZoneVersionName = envNamespace + "TMP_ZONE_VERSION_NAME"

	DefaultTempZoneVersionName = "lego_tmp"
)

// Config is used to configure the creation of the DNSProvider.
type Config struct {
	BaseURL             string
	APIToken            string
	TempZoneVersionName string
	HTTPClient          *http.Client
	PropagationTimeout  time.Duration
	PollingInterval     time.Duration
	TTL                 int
}

// NewDefaultConfig returns a default configuration for the DNSProvider.
func NewDefaultConfig() *Config {
	tr := &http.Transport{}

	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if ok {
		tr = defaultTransport.Clone()
	}

	return &Config{
		TTL:                 env.GetOrDefaultInt(EnvTTL, dns01.DefaultTTL),
		PropagationTimeout:  env.GetOrDefaultSecond(EnvPropagationTimeout, dns01.DefaultPropagationTimeout),
		PollingInterval:     env.GetOrDefaultSecond(EnvPollingInterval, dns01.DefaultPollingInterval),
		TempZoneVersionName: env.GetOrDefaultString(EnvTempZoneVersionName, DefaultTempZoneVersionName),
		HTTPClient: &http.Client{
			Timeout:   env.GetOrDefaultSecond(EnvHTTPTimeout, 10*time.Second),
			Transport: tr,
		},
	}
}

// DNSProvider implements the challenge.Provider interface.
type DNSProvider struct {
	config            *Config
	client            *internal.Client
	activeVersionUUID string
	tempVersionUUID   string
}

// NewDNSProvider returns a DNSProvider instance configured for Scaleway Dedibox API.
func NewDNSProvider() (*DNSProvider, error) {
	values, err := env.Get(EnvAPIToken)
	if err != nil {
		return nil, fmt.Errorf("scalewaydedibox: %w", err)
	}

	config := NewDefaultConfig()
	config.APIToken = values[EnvAPIToken]

	return NewDNSProviderConfig(config)
}

// NewDNSProviderConfig return a DNSProvider instance configured for Scaleway Dedibox API.
func NewDNSProviderConfig(config *Config) (*DNSProvider, error) {
	if config == nil {
		return nil, errors.New("scalewaydedibox: the configuration of the DNS provider is nil")
	}

	if config.APIToken == "" {
		return nil, errors.New("scalewaydedibox: credentials missing")
	}

	client, err := internal.NewClient(config.APIToken)
	if err != nil {
		return nil, fmt.Errorf("scalewaydedibox: %w", err)
	}

	client.HTTPClient = config.HTTPClient
	if err != nil {
		return nil, err
	}

	return &DNSProvider{
		client: client,
		config: config,
	}, nil
}

// Present creates a TXT record using the specified parameters.
func (d *DNSProvider) Present(domainName, token, keyAuth string) error {
	info := dns01.GetChallengeInfo(domainName, keyAuth)

	authZone, err := dns01.FindZoneByFqdn(info.EffectiveFQDN)
	if err != nil {
		return fmt.Errorf("scalewaydedibox: could not find zone for domain %q: %w", domainName, err)
	}

	ctx := context.Background()

	// ensure lego temporary zone version doesn´t already exist
	version, err := d.client.FindZoneVersion(ctx, authZone, d.config.TempZoneVersionName)
	if err != nil {
		return fmt.Errorf("scalewaydedibox: unable to verify version %s of zone %s already exist: %w", d.config.TempZoneVersionName, authZone, err)
	}
	if version != (internal.Version{}) {
		return fmt.Errorf("scalewaydedibox: a version named %s of zone %s already exists", d.config.TempZoneVersionName, authZone)
	}

	// find active version of zone
	activeZoneVersion, err := d.client.FindActiveZoneVersion(ctx, authZone)
	if err != nil {
		return fmt.Errorf("scalewaydedibox: unable to find active version of zone %s: %w", authZone, err)
	}
	d.activeVersionUUID = activeZoneVersion.UUIDRef

	// Create temporary zone version to host our challenge
	temporaryVersion, err := d.client.CreateZoneVersion(ctx, authZone, d.config.TempZoneVersionName)
	if err != nil {
		return fmt.Errorf("scalewaydedibox: unable to create temporary version %s of zone %s: %w", d.config.TempZoneVersionName, authZone, err)
	}
	d.tempVersionUUID = temporaryVersion.UUIDRef

	// Duplicate all records from active version to temporary version
	err = d.client.DuplicateZoneVersionRecords(ctx, authZone, activeZoneVersion.UUIDRef, temporaryVersion.UUIDRef)
	if err != nil {
		return fmt.Errorf("scalewaydedibox: unable to duplicate records from version %s to version %s: %w", activeZoneVersion.Name, temporaryVersion.Name, err)
	}

	// Create challenge record in temporary zone version
	recordName := strings.Replace(info.EffectiveFQDN, "."+authZone, "", 1)
	record := &internal.Record{Type: "TXT", Name: recordName, Value: info.Value, TTL: d.config.TTL}
	_, err = d.client.CreateRecord(ctx, authZone, temporaryVersion.UUIDRef, record)
	if err != nil {
		return fmt.Errorf("scalewaydedibox: unable to create record %s in version %s: %w", record.Name, temporaryVersion.Name, err)
	}

	// Enable zone version
	err = d.client.EnableZone(ctx, authZone, temporaryVersion.UUIDRef)
	if err != nil {
		return fmt.Errorf("scalewaydedibox: unable to enable version %s: %w", temporaryVersion.Name, err)
	}

	return nil
}

// CleanUp removes the TXT records matching the specified parameters.
func (d *DNSProvider) CleanUp(domainName, token, keyAuth string) error {
	info := dns01.GetChallengeInfo(domainName, keyAuth)

	authZone, err := dns01.FindZoneByFqdn(info.EffectiveFQDN)
	if err != nil {
		return fmt.Errorf("scalewaydedibox: could not find zone for domain %q: %w", domainName, err)
	}

	ctx := context.Background()

	// get the temporary zone info
	tempVersion, err := d.client.GetZoneVersion(ctx, authZone, d.tempVersionUUID)
	if err != nil {
		return fmt.Errorf("scalewaydedibox: unable to fetch version %s of zone %s: %w", d.tempVersionUUID, authZone, err)
	}
	if tempVersion == (internal.Version{}) {
		return fmt.Errorf("scalewaydedibox: no version found with UUID %s of zone %s", d.tempVersionUUID, authZone)
	}

	// get the real zone info
	realVersion, err := d.client.GetZoneVersion(ctx, authZone, d.activeVersionUUID)
	if err != nil {
		return fmt.Errorf("scalewaydedibox: unable to fetch version %s of zone %s: %w", d.activeVersionUUID, authZone, err)
	}
	if realVersion == (internal.Version{}) {
		return fmt.Errorf("scalewaydedibox: no version found with UUID %s of zone %s", d.activeVersionUUID, authZone)
	}

	// Enable back the real zone version
	err = d.client.EnableZone(ctx, authZone, d.activeVersionUUID)
	if err != nil {
		return fmt.Errorf("scalewaydedibox: unable to enable back version %s: %w", realVersion.Name, err)
	}

	// Delete temporary version
	err = d.client.DeleteZoneVersion(ctx, authZone, d.tempVersionUUID)
	if err != nil {
		return fmt.Errorf("scalewaydedibox: unable to delete version %s: %w", realVersion.Name, err)
	}

	return nil
}

// Timeout returns the timeout and interval to use when checking for DNS propagation.
// Adjusting here to cope with spikes in propagation times.
func (d *DNSProvider) Timeout() (timeout, interval time.Duration) {
	return d.config.PropagationTimeout, d.config.PollingInterval
}
