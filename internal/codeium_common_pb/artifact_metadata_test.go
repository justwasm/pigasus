package codeium_common_pb

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
	"time"
)

// TestArtifactMetadataFieldSet pins the recovered field set.
//
// 【反编译】 the authority is the getter set in the recovered symbol table:
// out/extracted-1.5.18/seeds/seeds.txt carries exactly seven
// (*ArtifactMetadata).Get* methods, and docs/legacy-re/extracts/
// priority-funcs-v1.3.4.txt carries the same seven. A field with no getter
// does not exist on the wire; a getter with no field is a reader the clone
// cannot answer.
//
// This struct used to declare Uri/Title/Kind/Author/Tags, none of which have
// a getter, and was missing Summary/Version/RequestFeedback/UserFacing, all
// of which do. That mismatch is why internal/app/artifacts kept a private
// sidecar struct instead of this message.
func TestArtifactMetadataFieldSet(t *testing.T) {
	want := []string{
		"ArtifactType",    // GetArtifactType
		"CreatedAt",       // GetCreatedAt
		"RequestFeedback", // GetRequestFeedback
		"Summary",         // GetSummary
		"UpdatedAt",       // GetUpdatedAt
		"UserFacing",      // GetUserFacing
		"Version",         // GetVersion
	}
	typ := reflect.TypeOf(ArtifactMetadata{})
	var got []string
	for i := 0; i < typ.NumField(); i++ {
		got = append(got, typ.Field(i).Name)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fields = %v\n want %v", got, want)
	}
}

// TestArtifactMetadataJSONNames pins the camelCase keys the reference
// actually reads and writes.
//
// artifacts.tmpl tells the model to set `ArtifactMetadata.UserFacing`;
// generative_ui/SKILL.md tells it to pass "UserFacing: true and
// RequestFeedback: false in ArtifactMetadata"; and the IDE bundle filters on
// `artifactMetadata?.userFacing` / `.requestFeedback`. The sidecar
// internal/app/artifacts writes must match key-for-key.
func TestArtifactMetadataJSONNames(t *testing.T) {
	ts := time.Date(2026, 11, 7, 13, 55, 31, 0, time.UTC)
	m := ArtifactMetadata{
		ArtifactType:    ArtifactTypeDocument,
		Summary:         "audit",
		CreatedAt:       ts,
		UpdatedAt:       ts,
		Version:         2,
		RequestFeedback: true,
		UserFacing:      true,
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"artifactType", "summary", "createdAt", "updatedAt",
		"version", "requestFeedback", "userFacing",
	} {
		if _, ok := got[key]; !ok {
			t.Errorf("marshalled JSON is missing %q: %s", key, b)
		}
	}
	// The keys the old struct invented must be gone.
	for _, stale := range []string{"uri", "title", "kind", "author", "tags"} {
		if _, ok := got[stale]; ok {
			t.Errorf("marshalled JSON still carries %q: %s", stale, b)
		}
	}
}

// TestArtifactMetadataGetterParity keeps a future edit from dropping a field
// whose getter the reference binary exports.
func TestArtifactMetadataGetterParity(t *testing.T) {
	getters := []string{
		"ArtifactType", "Summary", "CreatedAt", "UpdatedAt",
		"Version", "RequestFeedback", "UserFacing",
	}
	typ := reflect.TypeOf(ArtifactMetadata{})
	for _, name := range getters {
		if _, ok := typ.FieldByName(name); !ok {
			t.Errorf("missing field for recovered getter Get%s", name)
		}
	}
}
