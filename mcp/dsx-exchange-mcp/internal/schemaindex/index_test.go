// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package schemaindex

import "testing"

func TestDefaultDescribeBMSValueTopic(t *testing.T) {
	idx, err := Default()
	if err != nil {
		t.Fatalf("load default schema index: %v", err)
	}

	matches := idx.Describe("BMS/v1/PUB/Value/Rack/RackLiquidIsolationStatus/#")
	if len(matches) == 0 {
		t.Fatal("Describe returned no matches")
	}

	got := matches[0]
	if got.Domain != "bms" {
		t.Fatalf("domain = %q, want bms", got.Domain)
	}
	if got.Channel != "rackBmsValue" {
		t.Fatalf("channel = %q, want rackBmsValue", got.Channel)
	}
	if got.MatchedParameters["pointType"] != "RackLiquidIsolationStatus" {
		t.Fatalf("pointType = %q, want RackLiquidIsolationStatus", got.MatchedParameters["pointType"])
	}
	if len(got.RelatedTopics) != 1 || got.RelatedTopics[0].TopicFilter != "BMS/v1/PUB/Metadata/Rack/RackLiquidIsolationStatus/#" {
		t.Fatalf("related topics = %#v, want metadata counterpart", got.RelatedTopics)
	}
	if len(got.Messages) == 0 || got.Messages[0].Payload.Type != "object" {
		t.Fatalf("message payload summary = %#v, want object payload", got.Messages)
	}
}

func TestDefaultDescribePowerManagementTopic(t *testing.T) {
	idx, err := Default()
	if err != nil {
		t.Fatalf("load default schema index: %v", err)
	}

	matches := idx.Describe("grid/v1/poweragent/+/powerbreach")
	if len(matches) == 0 {
		t.Fatal("Describe returned no matches")
	}
	if got := matches[0].Domain; got != "power-management" {
		t.Fatalf("domain = %q, want power-management", got)
	}
	if got := matches[0].MatchedParameters["identifier"]; got != "+" {
		t.Fatalf("identifier parameter = %q, want +", got)
	}
}

func TestDefaultSearchBMSSelectorBuildsTopicFilter(t *testing.T) {
	idx, err := Default()
	if err != nil {
		t.Fatalf("load default schema index: %v", err)
	}

	matches := idx.Search(SearchOptions{
		Domain:     "bms",
		Role:       "value",
		ObjectType: "Rack",
		PointType:  "RackLiquidIsolationStatus",
		Limit:      10,
	})
	if len(matches) != 1 {
		t.Fatalf("Search returned %d matches, want 1: %#v", len(matches), matches)
	}
	if got := matches[0].TopicFilter; got != "BMS/v1/PUB/Value/Rack/RackLiquidIsolationStatus/#" {
		t.Fatalf("topic filter = %q, want BMS rack value filter", got)
	}
	if len(matches[0].RelatedTopics) != 1 || matches[0].RelatedTopics[0].TopicFilter != "BMS/v1/PUB/Metadata/Rack/RackLiquidIsolationStatus/#" {
		t.Fatalf("related topics = %#v, want metadata counterpart", matches[0].RelatedTopics)
	}
}

func TestDefaultSearchQueryFindsNICO(t *testing.T) {
	idx, err := Default()
	if err != nil {
		t.Fatalf("load default schema index: %v", err)
	}

	matches := idx.Search(SearchOptions{
		Domain: "nico",
		Query:  "state",
		Limit:  5,
	})
	if len(matches) == 0 {
		t.Fatal("Search returned no NICO state matches")
	}
	if matches[0].Domain != "nico" {
		t.Fatalf("domain = %q, want nico", matches[0].Domain)
	}
}
