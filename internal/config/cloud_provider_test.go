package config

import "testing"

func TestDetectCloudProvider(t *testing.T) {
	cases := []struct {
		url      string
		expected string
	}{
		{"https://akspriv-abc.hcp.brazilsouth.azmk8s.io:443", CloudProviderAKS},
		{"https://ABC123.gr7.us-east-1.eks.amazonaws.com", CloudProviderEKS},
		{"https://192.168.1.1:6443", CloudProviderUnknown},
		{"", CloudProviderUnknown},
	}
	for _, c := range cases {
		got := DetectCloudProvider(c.url)
		if got != c.expected {
			t.Errorf("DetectCloudProvider(%q) = %q, want %q", c.url, got, c.expected)
		}
	}
}

func TestExtractRegionFromEKSURL(t *testing.T) {
	cases := []struct {
		url    string
		region string
	}{
		{"https://ABC123.gr7.us-east-1.eks.amazonaws.com", "us-east-1"},
		{"https://DEF456.gr7.sa-east-1.eks.amazonaws.com", "sa-east-1"},
		{"https://akspriv-abc.hcp.brazilsouth.azmk8s.io", ""},
		{"", ""},
	}
	for _, c := range cases {
		got := ExtractRegionFromEKSURL(c.url)
		if got != c.region {
			t.Errorf("ExtractRegionFromEKSURL(%q) = %q, want %q", c.url, got, c.region)
		}
	}
}

func TestExtractAKSRegion(t *testing.T) {
	cases := []struct {
		url    string
		region string
	}{
		{"https://akspriv-abc.hcp.brazilsouth.azmk8s.io:443", "brazilsouth"},
		{"https://cluster.hcp.eastus.azmk8s.io", "eastus"},
		{"https://ABC123.gr7.us-east-1.eks.amazonaws.com", ""},
		{"", ""},
	}
	for _, c := range cases {
		got := extractAKSRegion(c.url)
		if got != c.region {
			t.Errorf("extractAKSRegion(%q) = %q, want %q", c.url, got, c.region)
		}
	}
}
