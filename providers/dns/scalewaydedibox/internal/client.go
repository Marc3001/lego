package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/go-acme/lego/v4/providers/dns/internal/errutils"
)

const (
	DefaultBaseURL        = "https://api.online.net/api/v1"
	DefaultRecordPriority = 12
)

// Client ScalewayDedibox client.
type Client struct {
	apiToken   string
	baseURL    *url.URL
	HTTPClient *http.Client
}

// NewClient creates a ScalewayDedibox client.
func NewClient(apiToken string) (*Client, error) {
	if apiToken == "" {
		return nil, errors.New("missing API token")
	}

	baseURL, _ := url.Parse(DefaultBaseURL)

	return &Client{
		apiToken:   apiToken,
		baseURL:    baseURL,
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}, nil
}

// ListAllVersions list all versions of a zone.
func (c *Client) ListAllVersions(ctx context.Context, authZone string) ([]Version, error) {
	domainName := authZone[0 : len(authZone)-1]
	endpoint := c.baseURL.JoinPath("domain", domainName, "version")

	var result []Version
	req, err := c.newJSONRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return result, err
	}
	err = c.do(req, &result)
	if err != nil {
		return result, err
	}

	return result, nil
}

// FindActiveZoneVersion find active version of zone.
func (c *Client) FindActiveZoneVersion(ctx context.Context, authZone string) (Version, error) {
	var result Version

	versions, err := c.ListAllVersions(ctx, authZone)
	if err != nil {
		return result, err
	}
	activeidx := slices.IndexFunc(versions, func(v Version) bool { return v.Active })
	result = versions[activeidx]

	return result, nil
}

// FindZoneVersion find version of zone from name.
func (c *Client) FindZoneVersion(ctx context.Context, authZone string, versionName string) (Version, error) {
	var result Version

	versions, err := c.ListAllVersions(ctx, authZone)
	if err != nil {
		return result, err
	}
	activeidx := slices.IndexFunc(versions, func(v Version) bool { return v.Name == versionName })
	if activeidx > -1 {
		result = versions[activeidx]
	}

	return result, nil
}

// GetZoneVersion get version of zone from UUID.
func (c *Client) GetZoneVersion(ctx context.Context, authZone string, versionUUID string) (Version, error) {
	domainName := authZone[0 : len(authZone)-1]
	endpoint := c.baseURL.JoinPath("domain", domainName, "version", versionUUID)

	var jsonResult Version
	req, err := c.newJSONRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return jsonResult, err
	}
	err = c.do(req, &jsonResult)
	if err != nil {
		return jsonResult, err
	}

	return jsonResult, nil
}

// CreateZoneVersion creates a new version of a zone.
func (c *Client) CreateZoneVersion(ctx context.Context, authZone string, versionName string) (Version, error) {
	domainName := authZone[0 : len(authZone)-1]
	endpoint := c.baseURL.JoinPath("domain", domainName, "version")
	payload := struct {
		Name string `json:"name"`
	}{Name: "lego_tmp"}

	var jsonResult Version
	req, err := c.newJSONRequest(ctx, http.MethodPost, endpoint, payload)
	if err != nil {
		return jsonResult, err
	}
	err = c.do(req, &jsonResult)
	if err != nil {
		return jsonResult, err
	}

	return jsonResult, nil
}

// DeleteZoneVersion deletes a version of a zone.
func (c *Client) DeleteZoneVersion(ctx context.Context, authZone string, versionUUID string) error {
	domainName := authZone[0 : len(authZone)-1]
	endpoint := c.baseURL.JoinPath("domain", domainName, "version", versionUUID)

	req, err := c.newJSONRequest(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	err = c.do(req, nil)
	if err != nil {
		return err
	}

	return nil
}

// GetAllRecords gets all records from zone version.
func (c *Client) GetAllRecords(ctx context.Context, authZone string, versionUUID string) ([]Record, error) {
	domainName := authZone[0 : len(authZone)-1]
	endpoint := c.baseURL.JoinPath("domain", domainName, "version", versionUUID, "zone")

	var jsonResult []Record
	req, err := c.newJSONRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return jsonResult, err
	}
	err = c.do(req, &jsonResult)
	if err != nil {
		return jsonResult, err
	}

	return jsonResult, nil
}

// GetRecord gets a record from zone version.
func (c *Client) GetRecord(ctx context.Context, authZone string, versionUUID string, recordName string, recordType string) (Record, error) {
	var result Record

	allRecords, err := c.GetAllRecords(ctx, authZone, versionUUID)
	if err != nil {
		return result, err
	}

	recordIdx := slices.IndexFunc(allRecords, func(r Record) bool { return r.Name == recordName && r.Type == recordType })
	if recordIdx > -1 {
		result = allRecords[recordIdx]
	}

	return result, nil
}

// ReadVersionUUID read versionUUID previously saved in TXT record.
func (c *Client) ReadVersionUUID(ctx context.Context, authZone string, versionUUID string) (string, error) {
	result := ""
	record, err := c.GetRecord(ctx, authZone, versionUUID, "lego", "TXT")
	if err != nil {
		return result, err
	}

	if strings.HasPrefix(record.Value, "active_version:") {
		result = record.Value[15:len(record.Value)]
	}
	return result, nil
}

// EnableZone enables a existing version of a zone.
func (c *Client) EnableZone(ctx context.Context, authZone string, versionID string) error {
	domainName := authZone[0 : len(authZone)-1]
	endpoint := c.baseURL.JoinPath("domain", domainName, "version", versionID, "enable")

	req, err := c.newJSONRequest(ctx, http.MethodPatch, endpoint, nil)
	if err != nil {
		return err
	}
	err = c.do(req, nil)
	if err != nil {
		return err
	}
	return nil
}

// CreateRecord creates a DNS record.
func (c *Client) CreateRecord(ctx context.Context, authZone string, versionID string, record *Record) (Record, error) {
	domainName := authZone[0 : len(authZone)-1]
	endpoint := c.baseURL.JoinPath("domain", domainName, "version", versionID, "zone")
	payload := Record{
		Name:     record.Name,
		Type:     record.Type,
		Priority: DefaultRecordPriority,
		TTL:      record.TTL,
		Value:    record.Value,
	}

	var result Record
	req, err := c.newJSONRequest(ctx, http.MethodPost, endpoint, payload)
	if err != nil {
		return result, err
	}
	err = c.do(req, &result)
	if err != nil {
		return result, err
	}
	return result, nil
}

// DuplicateZoneVersionRecords duplicates all records from source version to target version.
func (c *Client) DuplicateZoneVersionRecords(ctx context.Context, authZone string, sourceVersionUUID string, targetVersionUUID string) error {
	records, err := c.GetAllRecords(ctx, authZone, sourceVersionUUID)
	if err != nil {
		return err
	}

	allowedTypesRegexp := regexp.MustCompile("^(A)|(AAAA)|(CNAME)|(MX)|(SRV)|(TXT)$")
	for _, r := range records {
		if allowedTypesRegexp.MatchString(r.Type) {
			record := &Record{Type: r.Type, Name: r.Name, Value: r.Value, TTL: r.TTL}
			_, err := c.CreateRecord(ctx, authZone, targetVersionUUID, record)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *Client) do(req *http.Request, result any) error {
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return errutils.NewHTTPDoError(req, err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode/100 != 2 {
		return errutils.NewUnexpectedResponseStatusCodeError(req, resp)
	}

	if result == nil {
		return nil
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return errutils.NewReadResponseError(req, resp.StatusCode, err)
	}

	if err = json.Unmarshal(raw, result); err != nil {
		return errutils.NewUnmarshalError(req, resp.StatusCode, raw, err)
	}

	return nil
}

func (c *Client) newJSONRequest(ctx context.Context, method string, endpoint *url.URL, payload any) (*http.Request, error) {
	buf := new(bytes.Buffer)

	if payload != nil {
		err := json.NewEncoder(buf).Encode(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to create request JSON body: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), buf)
	if err != nil {
		return nil, fmt.Errorf("unable to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiToken)

	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return req, nil
}
