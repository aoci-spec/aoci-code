package mcptools

import (
	"strings"
	"testing"
)

func TestIncompleteHostConfirmationKeepsCompactionReloadAttempt(t *testing.T) {
	root := chunkSessionRepo(t)
	for _, name := range []string{"wrong hash", "wrong byte count", "missing end marker"} {
		t.Run(name, func(t *testing.T) {
			session := newCognitionRefreshSession()
			first, _ := chunkMetadata(t, sessionOverview(t, root, session, "host", compactionReload("all", "compact-1")))
			confirmation := &overviewHostConfirmation{
				Version: overviewDeliveryReceiptV1, BodySHA256: first["body_sha256"].(string),
				BodyBytes: int(first["body_utf8_bytes"].(float64)), EndMarkerObserved: true,
			}
			switch name {
			case "wrong hash":
				confirmation.BodySHA256 = strings.Repeat("0", 64)
			case "wrong byte count":
				confirmation.BodyBytes--
			case "missing end marker":
				confirmation.EndMarkerObserved = false
			}
			proof := resText(t, sessionOverview(t, root, session, "host", overviewIn{Scope: "all", HostConfirmation: confirmation}))
			if !strings.Contains(proof, "host_delivery_status: host_delivery_incomplete") ||
				!strings.Contains(proof, "model_attestation: not_provided") {
				t.Fatalf("expected an incomplete confirmation without a model proof:\n%.600s", proof)
			}
			stopText := resText(t, sessionOverview(t, root, session, "host", compactionReload("all", "compact-2")))
			if !strings.Contains(stopText, "delivery_mode: capacity_stop") {
				t.Fatalf("incomplete confirmation restarted the reload:\n%.300s", stopText)
			}
			stop := capacityStopFacts(t, stopText)
			if stop["delivery_mode"] != "capacity_stop" || stop["capacity_interruptions"] != "1" || stop["interrupted_chunks_served"] != "1" {
				t.Fatalf("incomplete confirmation lost the interrupted reload: %v", stop)
			}
		})
	}
}

func TestModelProofStillClosesCompactionReloadAttempt(t *testing.T) {
	root := chunkSessionRepo(t)
	for _, invalidHost := range []bool{false, true} {
		for _, failedModel := range []bool{false, true} {
			name := "model-only"
			if invalidHost {
				name = "mixed-incomplete-host"
			}
			if failedModel {
				name += "/failed-model"
			}
			t.Run(name, func(t *testing.T) {
				session := newCognitionRefreshSession()
				first, _ := chunkMetadata(t, sessionOverview(t, root, session, "host", compactionReload("all", "compact-1")))
				cursor := first["next_cursor"].(string)
				for cursor != "" {
					next, _ := chunkMetadata(t, sessionOverview(t, root, session, "host", overviewIn{Scope: "all", Cursor: cursor}))
					cursor, _ = next["next_cursor"].(string)
				}
				loaded, fail := loadCognitionCtx(root)
				if fail != nil {
					t.Fatal(fail)
				}
				view, err := loaded.set.Scope("all")
				if err != nil {
					t.Fatal(err)
				}
				_, sequence, err := buildVolumeOverviewBody(view)
				if err != nil {
					t.Fatal(err)
				}
				report := completeAttestation(buildOverviewChallenge(view.ScopeIdentity, sequence), view.ObjectCount, volumeScopeBytes(view)/3)
				if failedModel {
					for i := range report.ChallengeAnswers {
						report.ChallengeAnswers[i].Tag = "WRONG"
					}
				}
				input := overviewIn{Scope: "all", ModelAttestation: report}
				if invalidHost {
					input.HostConfirmation = &overviewHostConfirmation{
						Version: overviewDeliveryReceiptV1, BodySHA256: strings.Repeat("0", 64),
						BodyBytes: int(first["body_utf8_bytes"].(float64)), EndMarkerObserved: true,
					}
				}
				proof := resText(t, sessionOverview(t, root, session, "host", input))
				want := "model_attestation: pass"
				if failedModel {
					want = "model_attestation: fail"
				}
				if !strings.Contains(proof, want) {
					t.Fatalf("model proof did not exercise its expected result:\n%.600s", proof)
				}
				reload, _ := chunkMetadata(t, sessionOverview(t, root, session, "host", compactionReload("all", "compact-2")))
				if int(reload["chunk_index"].(float64)) != 1 || session.capacity != nil {
					t.Fatalf("model proof must keep its independent closure: %+v", reload)
				}
			})
		}
	}
}
