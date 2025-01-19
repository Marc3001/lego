package internal

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

type HTTPMock struct {
	pattern  string
	status   int
	filename string
}

func setupTest(t *testing.T, mocks []HTTPMock) *Client {
	t.Helper()

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	for _, m := range mocks {
		mux.HandleFunc(m.pattern, func(rw http.ResponseWriter, req *http.Request) {
			if m.filename == "" {
				rw.WriteHeader(m.status)
				return
			}

			file, err := os.Open(filepath.Join("fixtures", m.filename))
			if err != nil {
				http.Error(rw, err.Error(), http.StatusInternalServerError)
				return
			}

			defer func() { _ = file.Close() }()

			rw.WriteHeader(m.status)
			_, err = io.Copy(rw, file)
			if err != nil {
				http.Error(rw, err.Error(), http.StatusInternalServerError)
				return
			}
		})
	}

	client, _ := NewClient("token")

	client.HTTPClient = server.Client()
	client.baseURL, _ = url.Parse(server.URL)

	return client
}

func Test_ListAllVersions(t *testing.T) {
	mocks := []HTTPMock{
		{
			pattern:  "GET /domain/test.com/version",
			status:   http.StatusOK,
			filename: "domain_versions_all.json",
		},
	}
	client := setupTest(t, mocks)

	versions, err := client.ListAllVersions(context.Background(), "test.com.")

	assert.NoError(t, err)
	assert.Len(t, versions, 3)
}

func Test_FindActiveZoneVersion(t *testing.T) {
	mocks := []HTTPMock{
		{
			pattern:  "GET /domain/test.com/version",
			status:   http.StatusOK,
			filename: "domain_versions_all.json",
		},
	}
	client := setupTest(t, mocks)

	activeVersion, err := client.FindActiveZoneVersion(context.Background(), "test.com.")

	assert.NoError(t, err)
	assert.Equal(t, "activeVersion001", activeVersion.Name)
}

func Test_FindZoneVersion(t *testing.T) {
	mocks := []HTTPMock{
		{
			pattern:  "GET /domain/test.com/version",
			status:   http.StatusOK,
			filename: "domain_versions_all.json",
		},
	}
	client := setupTest(t, mocks)

	searchedVersion, err := client.FindZoneVersion(context.Background(), "test.com.", "notActiveVersion002")

	assert.NoError(t, err)
	assert.Equal(t, "notActiveVersion002", searchedVersion.Name)
}

func Test_CreateZoneVersion(t *testing.T) {
	mocks := []HTTPMock{
		{
			pattern:  "POST /domain/test.com/version",
			status:   http.StatusOK,
			filename: "create_version.json",
		},
	}
	client := setupTest(t, mocks)

	version, err := client.CreateZoneVersion(context.Background(), "test.com.", "lego_tmp")

	assert.NoError(t, err)
	assert.Equal(t, "lego_tmp", version.Name)
}

func Test_EnableZone(t *testing.T) {
	mocks := []HTTPMock{
		{
			pattern:  "PATCH /domain/test.com/version/313dbb10-75b9-4401-9fdb-d9149e5611eb/enable",
			status:   http.StatusOK,
			filename: "",
		},
	}
	client := setupTest(t, mocks)

	err := client.EnableZone(context.Background(), "test.com.", "313dbb10-75b9-4401-9fdb-d9149e5611eb")

	assert.NoError(t, err)
}

func Test_GetAllRecords(t *testing.T) {
	mocks := []HTTPMock{
		{
			pattern:  "GET /domain/test.com/version/313dbb10-75b9-4401-9fdb-d9149e5611eb/zone",
			status:   http.StatusOK,
			filename: "get_zone.json",
		},
	}
	client := setupTest(t, mocks)

	records, err := client.GetAllRecords(context.Background(), "test.com.", "313dbb10-75b9-4401-9fdb-d9149e5611eb")

	assert.NoError(t, err)
	assert.Len(t, records, 5)
}

func Test_GetRecord(t *testing.T) {
	mocks := []HTTPMock{
		{
			pattern:  "GET /domain/test.com/version/313dbb10-75b9-4401-9fdb-d9149e5611eb/zone",
			status:   http.StatusOK,
			filename: "get_zone.json",
		},
	}
	client := setupTest(t, mocks)

	record, err := client.GetRecord(context.Background(), "test.com.", "313dbb10-75b9-4401-9fdb-d9149e5611eb", "test04", "A")

	assert.NoError(t, err)
	assert.Equal(t, "A", record.Type)
	assert.Equal(t, "test04", record.Name)
	assert.Equal(t, 86400, record.TTL)
	assert.Equal(t, "127.0.0.1", record.Value)
}

func Test_ReadVersionUUID(t *testing.T) {
	mocks := []HTTPMock{
		{
			pattern:  "GET /domain/test.com/version/313dbb10-75b9-4401-9fdb-d9149e5611eb/zone",
			status:   http.StatusOK,
			filename: "get_zone.json",
		},
	}
	client := setupTest(t, mocks)

	UUID, err := client.ReadVersionUUID(context.Background(), "test.com.", "313dbb10-75b9-4401-9fdb-d9149e5611eb")

	assert.NoError(t, err)
	assert.Equal(t, "thisone", UUID)
}

func Test_CreateRecord(t *testing.T) {
	mocks := []HTTPMock{
		{
			pattern:  "POST /domain/test.com/version/313dbb10-75b9-4401-9fdb-d9149e5611eb/zone",
			status:   http.StatusOK,
			filename: "create_record.json",
		},
	}
	client := setupTest(t, mocks)

	record := Record{Type: "TXT", Name: "test", Value: "test_value", TTL: 300}
	result, err := client.CreateRecord(context.Background(), "test.com.", "313dbb10-75b9-4401-9fdb-d9149e5611eb", &record)

	assert.NoError(t, err)
	assert.Equal(t, "test", result.Name)
	assert.Equal(t, "TXT", result.Type)
	assert.Equal(t, "test_value", result.Value)
	assert.Equal(t, 300, result.TTL)
}

func Test_DuplicateZoneVersionRecords(t *testing.T) {
	mocks := []HTTPMock{
		{
			pattern:  "GET /domain/test.com/version/313dbb10-75b9-4401-9fdb-d9149e5611eb/zone",
			status:   http.StatusOK,
			filename: "get_zone.json",
		},
		{
			pattern:  "POST /domain/test.com/version/313dbb10-75b9-4401-9fdb-d9149e5611ec/zone",
			status:   http.StatusOK,
			filename: "create_record.json",
		},
	}
	client := setupTest(t, mocks)

	err := client.DuplicateZoneVersionRecords(context.Background(), "test.com.", "313dbb10-75b9-4401-9fdb-d9149e5611eb", "313dbb10-75b9-4401-9fdb-d9149e5611ec")

	assert.NoError(t, err)
}
