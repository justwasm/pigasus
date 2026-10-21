package language_server_pb

import (
	"context"
	"errors"
	"testing"
)

func TestDialLanguageServerService_MissingToken(t *testing.T) {
	_, err := DialLanguageServerService(DialOptions{Endpoint: "https://cloudcode-pa.googleapis.com"})
	if err == nil {
		t.Error("expected error when BearerToken func is missing")
	}
}

func TestSelectLicense(t *testing.T) {
	client, err := DialLanguageServerService(DialOptions{
		Endpoint:     "https://cloudcode-pa.googleapis.com",
		BearerToken:  func() (string, error) { return "test-token", nil },
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()
	resp, err := client.SelectLicense(context.Background(), &SelectLicenseRequest{
		LicenseTier: LicenseFree,
	})
	if err != nil {
		t.Fatalf("SelectLicense: %v", err)
	}
	if resp.LicenseTier != LicenseFree {
		t.Errorf("LicenseTier = %q, want %q", resp.LicenseTier, LicenseFree)
	}
	if resp.LicenseID == "" {
		t.Error("LicenseID is empty")
	}
}

func TestSelectLicense_EmptyTier(t *testing.T) {
	client, _ := DialLanguageServerService(DialOptions{
		Endpoint:    "https://cloudcode-pa.googleapis.com",
		BearerToken: func() (string, error) { return "test-token", nil },
	})
	defer client.Close()
	_, err := client.SelectLicense(context.Background(), &SelectLicenseRequest{})
	if !errors.Is(err, err) { // any error
		t.Logf("got err = %v", err)
	}
	if err == nil {
		t.Error("expected error for empty tier")
	}
}

func TestSelfAssignLicense(t *testing.T) {
	client, _ := DialLanguageServerService(DialOptions{
		Endpoint:    "https://cloudcode-pa.googleapis.com",
		BearerToken: func() (string, error) { return "test-token", nil },
	})
	defer client.Close()
	resp, err := client.SelfAssignLicense(context.Background(), &SelfAssignLicenseRequest{
		LicenseTier: LicenseStandard,
	})
	if err != nil {
		t.Fatalf("SelfAssignLicense: %v", err)
	}
	if resp.LicenseID == "" {
		t.Error("LicenseID is empty")
	}
}

func TestGetStatus(t *testing.T) {
	client, _ := DialLanguageServerService(DialOptions{
		Endpoint:    "https://cloudcode-pa.googleapis.com",
		BearerToken: func() (string, error) { return "test-token", nil },
	})
	defer client.Close()
	resp, err := client.GetStatus(context.Background(), &GetStatusRequest{})
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if !resp.Eligible {
		t.Error("Eligible = false, want true (placeholder)")
	}
	if resp.EndpointURL == "" {
		t.Error("EndpointURL is empty")
	}
}

func TestStreamGenerateContent(t *testing.T) {
	client, _ := DialLanguageServerService(DialOptions{
		Endpoint:    "https://cloudcode-pa.googleapis.com",
		BearerToken: func() (string, error) { return "test-token", nil },
	})
	defer client.Close()
	stream, err := client.StreamGenerateContent(context.Background(), &GenerateContentRequest{
		Model: "gemini-1.5-pro",
		Contents: []Content{{
			Role: "user",
			Parts: []Part{{Text: "hello"}},
		}},
	})
	if err != nil {
		t.Fatalf("StreamGenerateContent: %v", err)
	}
	count := 0
	for resp := range stream {
		count++
		if resp.ModelVersion != "gemini-1.5-pro" {
			t.Errorf("ModelVersion = %q, want gemini-1.5-pro", resp.ModelVersion)
		}
	}
	if count == 0 {
		t.Error("expected at least 1 response")
	}
}
