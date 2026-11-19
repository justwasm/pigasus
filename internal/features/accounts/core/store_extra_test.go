package core

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestSaveKeepsASectionThisBuildDoesNotKnow pins the registry's promise to the
// future: a key this build does not model survives a read-modify-write.
//
// Every writer here loads the whole registry, changes one field and saves the
// whole file. Without the preservation, a binary that predates a section
// deletes it on the next quota read — which is how a live `account_pool`
// disappeared every few seconds while an older `remote-control serve` was
// answering the Web UI's account list.
func TestSaveKeepsASectionThisBuildDoesNotKnow(t *testing.T) {
	accountsPath, _ := poolIsolate(t)
	writeFile(t, accountsPath, `{
  "version": 1,
  "active": "a@example.com",
  "accounts": [{"email": "a@example.com", "token": null}],
  "future_section": {"keep": ["me", 1, true]},
  "account_pool": {"threshold_5h": 0.05}
}`)

	if err := Update("a@example.com", func(a *Account) { a.PlanTier = "Google AI Pro" }); err != nil {
		t.Fatalf("Update: %v", err)
	}

	raw, err := os.ReadFile(accountsPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the registry is not JSON any more: %v\n%s", err, raw)
	}
	section, ok := doc["future_section"]
	if !ok {
		t.Fatalf("a save deleted a section this build does not model:\n%s", raw)
	}
	if got := string(section); !strings.Contains(got, `"keep"`) {
		t.Fatalf("the section was rewritten instead of kept: %s", got)
	}
	if _, ok := doc["account_pool"]; !ok {
		t.Fatalf("a save deleted the pool section:\n%s", raw)
	}
	if !strings.Contains(string(raw), "Google AI Pro") {
		t.Fatalf("the account change was not written:\n%s", raw)
	}
}

// TestSaveKeepsTheRegistryReadable pins that the preservation does not turn the
// file into something the registry cannot read back.
func TestSaveKeepsTheRegistryReadable(t *testing.T) {
	accountsPath, _ := poolIsolate(t)
	writeFile(t, accountsPath, `{"version":1,"active":"a@example.com","accounts":[{"email":"a@example.com","token":null}],"unknown":114}`)

	if err := Update("a@example.com", func(a *Account) { a.PlanTier = "Pro" }); err != nil {
		t.Fatalf("Update: %v", err)
	}
	st, err := Load()
	if err != nil {
		t.Fatalf("Load after a save with an unknown key: %v", err)
	}
	if st.Active != "a@example.com" || len(st.Accounts) != 1 {
		t.Fatalf("the registry did not round-trip: %+v", st)
	}
	acc, ok := st.Find("a@example.com")
	if !ok || acc.PlanTier != "Pro" {
		t.Fatalf("the change was lost: %+v", acc)
	}
}
