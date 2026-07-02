package service

import (
	"strings"
	"testing"
)

func TestExportComplianceCSV(t *testing.T) {
	svc := newTestService(t)
	report, err := svc.RunCompliance("test-user")
	if err != nil {
		t.Fatalf("RunCompliance: %v", err)
	}
	if len(report.Findings) == 0 {
		t.Fatal("expected at least one compliance finding")
	}
	data, ct, fn, err := svc.ExportComplianceReport("test-user", report.ID, "csv")
	if err != nil {
		t.Fatalf("ExportComplianceReport csv: %v", err)
	}
	if ct != "text/csv; charset=utf-8" {
		t.Errorf("unexpected content type: %s", ct)
	}
	if !strings.Contains(fn, "compliance-report-") || !strings.HasSuffix(fn, ".csv") {
		t.Errorf("unexpected filename: %s", fn)
	}
	if !strings.Contains(string(data), "类别") {
		t.Errorf("CSV should contain header row with 类别")
	}
	t.Logf("CSV (%d bytes):\n%s", len(data), string(data[:min(len(data), 500)]))
}

func TestExportComplianceHTML(t *testing.T) {
	svc := newTestService(t)
	report, err := svc.RunCompliance("test-user")
	if err != nil {
		t.Fatalf("RunCompliance: %v", err)
	}
	data, ct, fn, err := svc.ExportComplianceReport("test-user", report.ID, "html")
	if err != nil {
		t.Fatalf("ExportComplianceReport html: %v", err)
	}
	if ct != "text/html; charset=utf-8" {
		t.Errorf("unexpected content type: %s", ct)
	}
	if !strings.Contains(fn, "compliance-report-") || !strings.HasSuffix(fn, ".html") {
		t.Errorf("unexpected filename: %s", fn)
	}
	html := string(data)
	for _, required := range []string{"<!DOCTYPE html>", "<title>", "等保", "<table>", "</table>"} {
		if !strings.Contains(html, required) {
			t.Errorf("HTML should contain %q", required)
		}
	}
	t.Logf("HTML (%d bytes)", len(data))
}

func TestExportComplianceReportNotFound(t *testing.T) {
	svc := newTestService(t)
	_, _, _, err := svc.ExportComplianceReport("test-user", "nonexistent-id", "csv")
	if err == nil {
		t.Fatal("expected error for nonexistent report")
	}
}

func TestExportComplianceUnsupportedFormat(t *testing.T) {
	svc := newTestService(t)
	report, err := svc.RunCompliance("test-user")
	if err != nil {
		t.Fatalf("RunCompliance: %v", err)
	}
	_, _, _, err = svc.ExportComplianceReport("test-user", report.ID, "docx")
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
}

func TestBuildManifestEnclaveSecurityContext(t *testing.T) {
	svc := newTestService(t)
	snapshot := svc.Snapshot()
	manifest, err := svc.buildManifestFromSnapshot(snapshot, "cmp-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify isolation label
	if !strings.Contains(manifest, "isolation: enclave") {
		t.Errorf("expected isolation label in manifest, got:\n%s", manifest)
	}
	// Verify runtimeClassName
	if !strings.Contains(manifest, "runtimeClassName: rune") {
		t.Errorf("expected runtimeClassName: rune in manifest")
	}
	// Verify securityContext
	if !strings.Contains(manifest, "readOnlyRootFilesystem: true") {
		t.Errorf("expected readOnlyRootFilesystem: true")
	}
	if !strings.Contains(manifest, "runAsNonRoot: true") {
		t.Errorf("expected runAsNonRoot: true")
	}
	if !strings.Contains(manifest, "allowPrivilegeEscalation: false") {
		t.Errorf("expected allowPrivilegeEscalation: false")
	}
	if !strings.Contains(manifest, "seccompProfile:") {
		t.Errorf("expected seccompProfile")
	}
	// Verify drop capabilities
	if !strings.Contains(manifest, "- ALL") {
		t.Errorf("expected drop ALL capability")
	}
	t.Logf("Enclave manifest:\n%s", manifest)
}

func TestBuildManifestStandardNoSecurityContext(t *testing.T) {
	svc := newTestService(t)
	snapshot := svc.Snapshot()
	manifest, err := svc.buildManifestFromSnapshot(snapshot, "cmp-waf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Standard components should not have elevated security context
	if strings.Contains(manifest, "runAsNonRoot: true") {
		t.Errorf("standard component should not have runAsNonRoot")
	}
	if strings.Contains(manifest, "readOnlyRootFilesystem: true") {
		t.Errorf("standard component should not have readOnlyRootFilesystem")
	}
	// Verify isolation label
	if !strings.Contains(manifest, "isolation: standard") {
		t.Errorf("expected isolation label")
	}
	t.Logf("Standard manifest:\n%s", manifest)
}
