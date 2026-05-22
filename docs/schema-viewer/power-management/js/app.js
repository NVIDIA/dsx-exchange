
    const schema = {
  "asyncapi": "3.1.0",
  "info": {
    "title": "Power Management",
    "version": "1.7.0",
    "description": "Real-time power management and emergency control plane for data center grid events.\n\n## Protocol Binding\nThis API uses the MQTT Protocol Binding for CloudEvents v1.0.2:\n- https://github.com/cloudevents/spec/blob/v1.0.2/cloudevents/bindings/mqtt-protocol-binding.md\n\n**MQTT 3.1.1 Structured Content Mode:**\n- Content mode is always \"structured\" (MQTT 3.1.1 lacks custom metadata support)\n- The entire CloudEvents message is in the MQTT PUBLISH payload as JSON\n- Content-Type `application/cloudevents+json` is implied (no header in MQTT 3.1.1)\n- All CloudEvents attributes and data are in the JSON payload\n\n## CloudEvents Message Format\nAll messages conform to CloudEvents 1.0.2 specification with W3C Distributed Tracing Extension:\n- https://github.com/cloudevents/spec/blob/v1.0.2/cloudevents/spec.md\n- https://github.com/cloudevents/spec/blob/v1.0.2/cloudevents/extensions/distributed-tracing.md\n\nEach MQTT PUBLISH payload contains a JSON object with:\n- **specversion**: \"1.0.2\" (REQUIRED)\n- **id**: UUIDv4 unique event identifier (REQUIRED)\n- **source**: URI format //grid/v1/<role>/<identifier> (REQUIRED)\n- **type**: Event type e.g., grid.loadtarget.set.v1 (REQUIRED)\n- **time**: ISO 8601 timestamp (REQUIRED)\n- **datacontenttype**: \"application/json\" (REQUIRED)\n- **subject**: Resource subject e.g., loadtarget (REQUIRED)\n- **traceparent**: W3C Trace Context for distributed tracing (REQUIRED)\n- **tracestate**: Optional vendor-specific trace context\n- **correlationid**: UUIDv4 correlation identifier (OPTIONAL, CloudEvents extension)\n- **data**: JWS-signed payload (string - see Data Field Security below)\n\n## Source Format\nThe source attribute is a stable URI that identifies the publisher of an event.\nFormat: //<namespace>/<version>/<role>/<identifier>\n- namespace: grid\n- version: v1\n- role: isv | poweragent | infra\n- identifier: deployment-unique name assigned during registration\n\nExamples:\n- ISV: //grid/v1/isv/acme-energy\n- Power Agent: //grid/v1/poweragent/dps-prod\n- Infra Agent: //grid/v1/infra/ima-prod\n\n## Event Types\n- grid.loadtarget.set.v1 — ISV sets load target (fire-and-forget command)\n- grid.powerstate.status.v1 — Power Agent publishes power state snapshots\n- grid.powerbreach.alert.v1 — Power Agent publishes breach alerts\n- grid.powerbreach.enforcement.v1 — Infra Agent publishes enforcement outcomes\n\n## Security\n- Transport: MQTT 3.1.1 with TLS (mTLS or OAuth2)\n- Data Field: MUST be signed (JWS RFC 7515) — broker never sees verified plaintext\n\n## Data Field Security (JWS)\nThe `data` field uses cryptographic protection per RFC 7515 (JWS):\n\n1. **Sign (JWS)**: The JSON payload is signed using JWS (JSON Web Signature)\n   - Algorithm: ES256 (ECDSA using P-256 and SHA-256) or RS256\n   - Provides: Data integrity and authenticity\n   - The `kid` (Key ID) in JWS header identifies the signing key\n\n2. **Result**: The `data` field contains a JWS compact serialization string:\n   `<JWS Header>.<Payload>.<Signature>`\n\n**Processing Order:**\n- Sender: JSON payload → JWS sign → data field\n- Receiver: data field → JWS verify → JSON payload\n\n## Distributed Tracing\nAll participants MUST propagate traceparent/tracestate for end-to-end visibility.\n",
    "contact": {
      "name": "Grid Platform Team",
      "email": "grid-platform@nvidia.com"
    }
  },
  "defaultContentType": "application/cloudevents+json",
  "servers": {
    "production": {
      "host": "broker.example.com",
      "protocol": "mqtt",
      "description": "MQTT broker for power management grid events"
    }
  },
  "channels": {
    "loadTargetSetChannel": {
      "address": "grid/v1/isv/{identifier}/loadtarget/set",
      "parameters": {
        "identifier": {
          "description": "Unique identifier of the ISV, power agent, or infrastructure agent."
        }
      },
      "messages": {
        "loadTargetSet": {
          "name": "LoadTargetSetMessage",
          "title": "Set Load Target",
          "summary": "ISV publishes load target command to the event bus.",
          "contentType": "application/cloudevents+json",
          "payload": {
            "description": "CloudEvents envelope for grid.loadtarget.set.v1.",
            "allOf": [
              {
                "type": "object",
                "description": "Common CloudEvents 1.0.2 attributes shared by all events.\nEach event type extends this with a specific \"data\" field.\n\nThe \"data\" field is transmitted as a JWS compact serialization string.\nThe plaintext JSON payload is signed with JWS (RFC 7515).\n",
                "required": [
                  "specversion",
                  "id",
                  "source",
                  "type",
                  "time",
                  "datacontenttype",
                  "subject",
                  "traceparent"
                ],
                "properties": {
                  "specversion": {
                    "type": "string",
                    "const": "1.0.2",
                    "description": "MUST be \"1.0.2\" — version of the CloudEvents specification.",
                    "x-parser-schema-id": "<anonymous-schema-2>"
                  },
                  "id": {
                    "type": "string",
                    "format": "uuid",
                    "description": "MUST be a UUIDv4 — globally unique identifier for this event instance.",
                    "x-parser-schema-id": "<anonymous-schema-3>"
                  },
                  "source": {
                    "type": "string",
                    "pattern": "^//grid/v1/(isv|poweragent|infra)/[^/]+$",
                    "description": "Stable URI identifying the publisher. Format: //grid/v1/<role>/<identifier>.\nExamples:\n  - //grid/v1/isv/acme-energy\n  - //grid/v1/poweragent/dps-prod\n  - //grid/v1/infra/ima-prod\n",
                    "x-parser-schema-id": "<anonymous-schema-4>"
                  },
                  "type": {
                    "type": "string",
                    "description": "Event type following grid.<entity>.<action>.v1 or grid.<entity>.status.v1.",
                    "x-parser-schema-id": "<anonymous-schema-5>"
                  },
                  "time": {
                    "type": "string",
                    "format": "date-time",
                    "description": "MUST be an ISO 8601 timestamp — time the event was created.",
                    "x-parser-schema-id": "<anonymous-schema-6>"
                  },
                  "datacontenttype": {
                    "type": "string",
                    "const": "application/json",
                    "description": "MUST be \"application/json\" — media type of the data payload.",
                    "x-parser-schema-id": "<anonymous-schema-7>"
                  },
                  "subject": {
                    "type": "string",
                    "description": "Resource subject identifying the domain object (e.g., loadtarget, powerstate, powerbreach).",
                    "x-parser-schema-id": "<anonymous-schema-8>"
                  },
                  "correlationid": {
                    "type": "string",
                    "format": "uuid",
                    "description": "CloudEvents extension attribute. UUIDv4 correlation identifier.\nGenerated by the ISV on grid.loadtarget.set.v1. Echoed by the\nPower Management Agent in status events for correlation.\n",
                    "x-parser-schema-id": "<anonymous-schema-9>"
                  },
                  "traceparent": {
                    "type": "string",
                    "pattern": "^[0-9a-f]{2}-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$",
                    "description": "REQUIRED per W3C Trace Context and CloudEvents Distributed Tracing Extension.\nFormat: version-traceid-parentid-flags.\n",
                    "x-parser-schema-id": "<anonymous-schema-10>"
                  },
                  "tracestate": {
                    "type": "string",
                    "description": "OPTIONAL per W3C Trace Context. Carries vendor-specific contextual\ninformation across tracing systems.\n",
                    "x-parser-schema-id": "<anonymous-schema-11>"
                  }
                },
                "x-parser-schema-id": "BaseCloudEvent"
              },
              {
                "type": "object",
                "required": [
                  "data"
                ],
                "properties": {
                  "type": {
                    "const": "grid.loadtarget.set.v1",
                    "x-parser-schema-id": "<anonymous-schema-13>"
                  },
                  "data": {
                    "description": "WIRE FORMAT: JWS compact serialization string (type: string).\nPLAINTEXT CONTENT: After JWS verification, the payload conforms\nto SetLoadTargetData schema (type: object).\n",
                    "oneOf": [
                      {
                        "type": "string",
                        "description": "JWS Compact Serialization (RFC 7515) containing a signed payload.\nThis is the WIRE FORMAT for the \"data\" field in CloudEvents messages.\n\nStructure: BASE64URL(JWS Header).BASE64URL(Payload).BASE64URL(Signature)\n\nJWS Header (example):\n{\n  \"alg\": \"ES256\",\n  \"kid\": \"<signing-key-id>\"\n}\n\nThe payload, when verified, contains the JSON event data\nas defined by the message type schema.\n",
                        "pattern": "^[A-Za-z0-9_-]+\\.[A-Za-z0-9_-]+\\.[A-Za-z0-9_-]+$",
                        "x-parser-schema-id": "JwsCompactSerialization"
                      },
                      {
                        "type": "object",
                        "description": "Payload for grid.loadtarget.set.v1.",
                        "required": [
                          "targets"
                        ],
                        "properties": {
                          "targets": {
                            "type": "array",
                            "description": "List of load target requests.",
                            "minItems": 1,
                            "items": {
                              "type": "object",
                              "required": [
                                "interval",
                                "load_constraint"
                              ],
                              "properties": {
                                "interval": {
                                  "type": "object",
                                  "description": "Time window for load target scheduling.",
                                  "properties": {
                                    "start_time": {
                                      "type": "string",
                                      "format": "date-time",
                                      "description": "ISO 8601 UTC start time. Defaults to now if omitted.",
                                      "x-parser-schema-id": "<anonymous-schema-17>"
                                    },
                                    "end_time": {
                                      "type": "string",
                                      "format": "date-time",
                                      "description": "ISO 8601 UTC end time. If omitted, target remains in effect until replaced.",
                                      "x-parser-schema-id": "<anonymous-schema-18>"
                                    }
                                  },
                                  "x-parser-schema-id": "Interval"
                                },
                                "feed_tags": {
                                  "type": "array",
                                  "description": "List of feed identifiers this target applies to (e.g., [\"main-a\"]).\nIf omitted or empty, the target applies to all feeds.\n",
                                  "items": {
                                    "type": "string",
                                    "x-parser-schema-id": "<anonymous-schema-19>"
                                  },
                                  "examples": [
                                    [
                                      "main-a",
                                      "main-b"
                                    ]
                                  ],
                                  "x-parser-schema-id": "FeedTags"
                                },
                                "load_constraint": {
                                  "description": "Power constraint to apply. If null, removes any active constraint\nand reverts the feed to its unconstrained default load level.\n",
                                  "oneOf": [
                                    {
                                      "type": "object",
                                      "description": "Power limit constraint.",
                                      "required": [
                                        "value",
                                        "unit"
                                      ],
                                      "properties": {
                                        "value": {
                                          "type": "number",
                                          "format": "double",
                                          "minimum": 0,
                                          "description": "Target power limit (must be > 0).",
                                          "x-parser-schema-id": "<anonymous-schema-21>"
                                        },
                                        "unit": {
                                          "type": "string",
                                          "enum": [
                                            "watt",
                                            "kilowatt",
                                            "megawatt"
                                          ],
                                          "description": "Unit of measurement.",
                                          "x-parser-schema-id": "<anonymous-schema-22>"
                                        }
                                      },
                                      "x-parser-schema-id": "LoadConstraint"
                                    },
                                    {
                                      "type": "null",
                                      "x-parser-schema-id": "<anonymous-schema-23>"
                                    }
                                  ],
                                  "x-parser-schema-id": "<anonymous-schema-20>"
                                },
                                "strategy": {
                                  "type": "object",
                                  "description": "Load shedding strategy configuration.",
                                  "properties": {
                                    "best_effort": {
                                      "type": "boolean",
                                      "default": false,
                                      "description": "If true, DPS sheds load non-disruptively by adjusting DPM-enabled jobs.\nIf the constraint cannot be satisfied, DPS sheds as much as possible and\nleaves the feed in a non-compliant state until workloads terminate.\n",
                                      "x-parser-schema-id": "<anonymous-schema-24>"
                                    }
                                  },
                                  "x-parser-schema-id": "Strategy"
                                }
                              },
                              "x-parser-schema-id": "<anonymous-schema-16>"
                            },
                            "x-parser-schema-id": "<anonymous-schema-15>"
                          }
                        },
                        "x-parser-schema-id": "SetLoadTargetData"
                      }
                    ],
                    "x-parser-schema-id": "<anonymous-schema-14>"
                  }
                },
                "x-parser-schema-id": "<anonymous-schema-12>"
              }
            ],
            "x-parser-schema-id": "LoadTargetSetEvent"
          },
          "x-parser-unique-object-id": "loadTargetSet"
        }
      },
      "description": "ISV publishes load target commands. Fire-and-forget pattern — the ISV\ndoes not wait for a response. Confirmation comes via grid.powerstate.status.v1.\n",
      "bindings": {
        "mqtt": {
          "qos": 1
        }
      },
      "x-parser-unique-object-id": "loadTargetSetChannel"
    },
    "powerStateStatusChannel": {
      "address": "grid/v1/poweragent/{identifier}/powerstate/status",
      "parameters": {
        "identifier": "$ref:$.channels.loadTargetSetChannel.parameters.identifier"
      },
      "messages": {
        "powerStateStatus": {
          "name": "PowerStateStatusMessage",
          "title": "Power State Status",
          "summary": "Power Management Agent publishes full power state snapshot.",
          "contentType": "application/cloudevents+json",
          "payload": {
            "description": "CloudEvents envelope for grid.powerstate.status.v1.",
            "allOf": [
              "$ref:$.channels.loadTargetSetChannel.messages.loadTargetSet.payload.allOf[0]",
              {
                "type": "object",
                "required": [
                  "data"
                ],
                "properties": {
                  "type": {
                    "const": "grid.powerstate.status.v1",
                    "x-parser-schema-id": "<anonymous-schema-27>"
                  },
                  "data": {
                    "description": "WIRE FORMAT: JWS compact serialization string (type: string).\nPLAINTEXT CONTENT: After JWS verification, the payload conforms\nto PowerStateStatusData schema (type: object).\n",
                    "oneOf": [
                      "$ref:$.channels.loadTargetSetChannel.messages.loadTargetSet.payload.allOf[1].properties.data.oneOf[0]",
                      {
                        "type": "object",
                        "description": "Payload for grid.powerstate.status.v1.",
                        "required": [
                          "snapshot_time",
                          "event",
                          "feeds"
                        ],
                        "properties": {
                          "snapshot_time": {
                            "type": "string",
                            "format": "date-time",
                            "description": "ISO 8601 UTC timestamp of this snapshot.",
                            "x-parser-schema-id": "<anonymous-schema-29>"
                          },
                          "event": {
                            "type": "string",
                            "enum": [
                              "target_set",
                              "start_ramp_down",
                              "end_ramp_down",
                              "start_ramp_up",
                              "end_ramp_up",
                              "periodic"
                            ],
                            "description": "The trigger that caused this status to be published:\n- target_set — a grid.loadtarget.set.v1 was received and schedule updated\n- start_ramp_down — constraint applied or tightened, redistribution begins\n- end_ramp_down — redistribution complete, load converged to target\n- start_ramp_up — constraint relaxed or removed, redistribution begins\n- end_ramp_up — redistribution complete\n- periodic — regular heartbeat, no state change\n",
                            "x-parser-schema-id": "<anonymous-schema-30>"
                          },
                          "feeds": {
                            "type": "object",
                            "description": "Map of feed_tag to power state.",
                            "additionalProperties": {
                              "type": "object",
                              "description": "Current power state for a single feed.",
                              "required": [
                                "metadata",
                                "targets",
                                "calculated_load",
                                "in_flight",
                                "compliant"
                              ],
                              "properties": {
                                "metadata": {
                                  "type": "object",
                                  "description": "Static feed configuration. Values don't change during a curtailment event.",
                                  "required": [
                                    "power_minimum",
                                    "power_maximum",
                                    "default_constraint"
                                  ],
                                  "properties": {
                                    "power_minimum": {
                                      "allOf": [
                                        {
                                          "type": "object",
                                          "description": "Power measurement with value and unit.",
                                          "required": [
                                            "value",
                                            "unit"
                                          ],
                                          "properties": {
                                            "value": {
                                              "type": "number",
                                              "format": "double",
                                              "minimum": 0,
                                              "description": "Power value (must be >= 0).",
                                              "x-parser-schema-id": "<anonymous-schema-33>"
                                            },
                                            "unit": {
                                              "type": "string",
                                              "enum": [
                                                "watt",
                                                "kilowatt",
                                                "megawatt"
                                              ],
                                              "description": "Unit of measurement.",
                                              "x-parser-schema-id": "<anonymous-schema-34>"
                                            }
                                          },
                                          "x-parser-schema-id": "PowerUnit"
                                        }
                                      ],
                                      "description": "Minimum power threshold for this feed.",
                                      "x-parser-schema-id": "<anonymous-schema-32>"
                                    },
                                    "power_maximum": {
                                      "allOf": [
                                        "$ref:$.channels.powerStateStatusChannel.messages.powerStateStatus.payload.allOf[1].properties.data.oneOf[1].properties.feeds.additionalProperties.properties.metadata.properties.power_minimum.allOf[0]"
                                      ],
                                      "description": "Maximum power capacity for this feed.",
                                      "x-parser-schema-id": "<anonymous-schema-35>"
                                    },
                                    "default_constraint": {
                                      "allOf": [
                                        "$ref:$.channels.powerStateStatusChannel.messages.powerStateStatus.payload.allOf[1].properties.data.oneOf[1].properties.feeds.additionalProperties.properties.metadata.properties.power_minimum.allOf[0]"
                                      ],
                                      "description": "Default power constraint when no load target is active.",
                                      "x-parser-schema-id": "<anonymous-schema-36>"
                                    }
                                  },
                                  "x-parser-schema-id": "FeedMetadata"
                                },
                                "targets": {
                                  "type": "array",
                                  "description": "All load targets for this feed — active and scheduled. Empty array if no targets exist.",
                                  "items": {
                                    "type": "object",
                                    "description": "A load target for a feed — active or scheduled.",
                                    "required": [
                                      "active",
                                      "load_constraint",
                                      "interval",
                                      "strategy",
                                      "correlation_id"
                                    ],
                                    "properties": {
                                      "active": {
                                        "type": "boolean",
                                        "description": "true if this target is currently being enforced.",
                                        "x-parser-schema-id": "<anonymous-schema-38>"
                                      },
                                      "load_constraint": "$ref:$.channels.loadTargetSetChannel.messages.loadTargetSet.payload.allOf[1].properties.data.oneOf[1].properties.targets.items.properties.load_constraint.oneOf[0]",
                                      "interval": "$ref:$.channels.loadTargetSetChannel.messages.loadTargetSet.payload.allOf[1].properties.data.oneOf[1].properties.targets.items.properties.interval",
                                      "strategy": "$ref:$.channels.loadTargetSetChannel.messages.loadTargetSet.payload.allOf[1].properties.data.oneOf[1].properties.targets.items.properties.strategy",
                                      "correlation_id": {
                                        "type": "string",
                                        "format": "uuid",
                                        "description": "The correlationid from the grid.loadtarget.set.v1 that created this target.",
                                        "x-parser-schema-id": "<anonymous-schema-39>"
                                      }
                                    },
                                    "x-parser-schema-id": "FeedTarget"
                                  },
                                  "x-parser-schema-id": "<anonymous-schema-37>"
                                },
                                "calculated_load": {
                                  "allOf": [
                                    "$ref:$.channels.powerStateStatusChannel.messages.powerStateStatus.payload.allOf[1].properties.data.oneOf[1].properties.feeds.additionalProperties.properties.metadata.properties.power_minimum.allOf[0]"
                                  ],
                                  "description": "Agent-calculated effective load after shedding strategy is applied.\nMay differ from actual measured power during ramp transitions.\n",
                                  "x-parser-schema-id": "<anonymous-schema-40>"
                                },
                                "in_flight": {
                                  "type": "boolean",
                                  "description": "true if the agent is actively transitioning between power levels.",
                                  "x-parser-schema-id": "<anonymous-schema-41>"
                                },
                                "power_event_start_time": {
                                  "type": "string",
                                  "format": "date-time",
                                  "description": "ISO 8601 UTC timestamp when the current power event began. null if no event is active.",
                                  "x-parser-schema-id": "<anonymous-schema-42>"
                                },
                                "compliant": {
                                  "type": "boolean",
                                  "description": "true if the calculated load is within the active target constraint. true when no target is active.",
                                  "x-parser-schema-id": "<anonymous-schema-43>"
                                }
                              },
                              "x-parser-schema-id": "FeedState"
                            },
                            "x-parser-schema-id": "<anonymous-schema-31>"
                          }
                        },
                        "x-parser-schema-id": "PowerStateStatusData"
                      }
                    ],
                    "x-parser-schema-id": "<anonymous-schema-28>"
                  }
                },
                "x-parser-schema-id": "<anonymous-schema-26>"
              }
            ],
            "x-parser-schema-id": "PowerStateStatusEvent"
          },
          "x-parser-unique-object-id": "powerStateStatus"
        }
      },
      "description": "Power Management Agent publishes full snapshots of current power state\nfor all feeds. Includes compliance, ramp state, and active/scheduled targets.\nPublished on state transitions and as periodic heartbeats.\n",
      "bindings": {
        "mqtt": {
          "qos": 1
        }
      },
      "x-parser-unique-object-id": "powerStateStatusChannel"
    },
    "powerBreachAlertChannel": {
      "address": "grid/v1/poweragent/{identifier}/powerbreach",
      "parameters": {
        "identifier": "$ref:$.channels.loadTargetSetChannel.parameters.identifier"
      },
      "messages": {
        "powerBreachAlert": {
          "name": "PowerBreachAlertMessage",
          "title": "Power Breach Alert",
          "summary": "Power Management Agent publishes breach alert with shed hints.",
          "contentType": "application/cloudevents+json",
          "payload": {
            "description": "CloudEvents envelope for grid.powerbreach.alert.v1.",
            "allOf": [
              "$ref:$.channels.loadTargetSetChannel.messages.loadTargetSet.payload.allOf[0]",
              {
                "type": "object",
                "required": [
                  "data"
                ],
                "properties": {
                  "type": {
                    "const": "grid.powerbreach.alert.v1",
                    "x-parser-schema-id": "<anonymous-schema-46>"
                  },
                  "data": {
                    "description": "WIRE FORMAT: JWS compact serialization string (type: string).\nPLAINTEXT CONTENT: After JWS verification, the payload conforms\nto PowerBreachAlertData schema (type: object).\n",
                    "oneOf": [
                      "$ref:$.channels.loadTargetSetChannel.messages.loadTargetSet.payload.allOf[1].properties.data.oneOf[0]",
                      {
                        "type": "object",
                        "description": "Payload for grid.powerbreach.alert.v1.",
                        "required": [
                          "feed_tag",
                          "status",
                          "severity",
                          "breach",
                          "target",
                          "measured_load",
                          "infrastructure_actions"
                        ],
                        "properties": {
                          "feed_tag": {
                            "type": "string",
                            "description": "The feed identifier where the breach was detected.",
                            "x-parser-schema-id": "<anonymous-schema-48>"
                          },
                          "status": {
                            "type": "string",
                            "enum": [
                              "active",
                              "escalated",
                              "resolved"
                            ],
                            "description": "Breach lifecycle state.",
                            "x-parser-schema-id": "<anonymous-schema-49>"
                          },
                          "severity": {
                            "type": "string",
                            "enum": [
                              "warning",
                              "critical"
                            ],
                            "description": "Breach severity level:\n- warning — excess between threshold and 20% over target\n- critical — excess exceeds 20% over target\n",
                            "x-parser-schema-id": "<anonymous-schema-50>"
                          },
                          "breach": {
                            "type": "object",
                            "description": "Breach identification and timing.",
                            "required": [
                              "breach_id",
                              "detected_at"
                            ],
                            "properties": {
                              "breach_id": {
                                "type": "string",
                                "format": "uuid",
                                "description": "Unique identifier for this breach instance (UUIDv4). Remains the same\nacross active → escalated → resolved for the same breach.\n",
                                "x-parser-schema-id": "<anonymous-schema-51>"
                              },
                              "detected_at": {
                                "type": "string",
                                "format": "date-time",
                                "description": "ISO 8601 UTC timestamp when the breach was first detected.",
                                "x-parser-schema-id": "<anonymous-schema-52>"
                              },
                              "resolved_at": {
                                "type": "string",
                                "format": "date-time",
                                "description": "ISO 8601 UTC timestamp when the breach was resolved. Present only when status is resolved.",
                                "x-parser-schema-id": "<anonymous-schema-53>"
                              }
                            },
                            "x-parser-schema-id": "BreachDetails"
                          },
                          "target": {
                            "type": "object",
                            "description": "The load target being breached.",
                            "required": [
                              "correlation_id",
                              "load_constraint"
                            ],
                            "properties": {
                              "correlation_id": {
                                "type": "string",
                                "format": "uuid",
                                "description": "The correlationid from the grid.loadtarget.set.v1 that established this target.",
                                "x-parser-schema-id": "<anonymous-schema-54>"
                              },
                              "load_constraint": "$ref:$.channels.loadTargetSetChannel.messages.loadTargetSet.payload.allOf[1].properties.data.oneOf[1].properties.targets.items.properties.load_constraint.oneOf[0]"
                            },
                            "x-parser-schema-id": "BreachTarget"
                          },
                          "measured_load": {
                            "allOf": [
                              "$ref:$.channels.powerStateStatusChannel.messages.powerStateStatus.payload.allOf[1].properties.data.oneOf[1].properties.feeds.additionalProperties.properties.metadata.properties.power_minimum.allOf[0]"
                            ],
                            "description": "Actual measured power at time of event.",
                            "x-parser-schema-id": "<anonymous-schema-55>"
                          },
                          "shed_hints": {
                            "type": "array",
                            "description": "Ordered list of load shedding recommendations. Present when status\nis active or escalated. Absent when resolved.\n",
                            "items": {
                              "type": "object",
                              "description": "Resource-level load shedding recommendation. Shed hints are non-binding\nadvisory suggestions. The Infrastructure Management Agent MAY override\nor augment based on topology and operational criteria.\n",
                              "required": [
                                "resource_id",
                                "resource_type",
                                "action",
                                "priority"
                              ],
                              "properties": {
                                "resource_id": {
                                  "type": "string",
                                  "description": "Identifier of the resource to throttle.",
                                  "x-parser-schema-id": "<anonymous-schema-57>"
                                },
                                "resource_type": {
                                  "type": "string",
                                  "enum": [
                                    "node",
                                    "gpu",
                                    "chassis"
                                  ],
                                  "description": "Type of resource.",
                                  "x-parser-schema-id": "<anonymous-schema-58>"
                                },
                                "action": {
                                  "type": "string",
                                  "enum": [
                                    "power_cap",
                                    "throttle",
                                    "suspend"
                                  ],
                                  "description": "Recommended action.",
                                  "x-parser-schema-id": "<anonymous-schema-59>"
                                },
                                "priority": {
                                  "type": "integer",
                                  "minimum": 1,
                                  "description": "Shedding priority (1 = shed first, higher = shed later).",
                                  "x-parser-schema-id": "<anonymous-schema-60>"
                                },
                                "target_power": {
                                  "allOf": [
                                    "$ref:$.channels.powerStateStatusChannel.messages.powerStateStatus.payload.allOf[1].properties.data.oneOf[1].properties.feeds.additionalProperties.properties.metadata.properties.power_minimum.allOf[0]"
                                  ],
                                  "description": "Suggested power cap. Present when action is power_cap.",
                                  "x-parser-schema-id": "<anonymous-schema-61>"
                                }
                              },
                              "x-parser-schema-id": "ShedHint"
                            },
                            "x-parser-schema-id": "<anonymous-schema-56>"
                          },
                          "infrastructure_actions": {
                            "type": "array",
                            "description": "Ordered list of escalating infrastructure-level actions. Empty array\nwhen status is resolved. Ordered from least to most disruptive.\n",
                            "items": {
                              "type": "string",
                              "enum": [
                                "SHUTDOWN_NODES",
                                "SHUTDOWN_RACKS",
                                "CUT_DATA_FEEDS",
                                "TRIP_BREAKERS"
                              ],
                              "x-parser-schema-id": "<anonymous-schema-63>"
                            },
                            "x-parser-schema-id": "<anonymous-schema-62>"
                          }
                        },
                        "x-parser-schema-id": "PowerBreachAlertData"
                      }
                    ],
                    "x-parser-schema-id": "<anonymous-schema-47>"
                  }
                },
                "x-parser-schema-id": "<anonymous-schema-45>"
              }
            ],
            "x-parser-schema-id": "PowerBreachAlertEvent"
          },
          "x-parser-unique-object-id": "powerBreachAlert"
        }
      },
      "description": "Power Management Agent publishes breach alerts when measured power exceeds\nthe active load target. Breach lifecycle: active → escalated → resolved.\nAll events in a single breach share the same breach_id.\n",
      "bindings": {
        "mqtt": {
          "qos": 2
        }
      },
      "x-parser-unique-object-id": "powerBreachAlertChannel"
    },
    "powerBreachEnforcementChannel": {
      "address": "grid/v1/infra/{infraAgentId}/powerbreach/enforcement",
      "parameters": {
        "infraAgentId": {
          "description": "Unique identifier of the Infrastructure Management Agent."
        }
      },
      "messages": {
        "powerBreachEnforcement": {
          "name": "PowerBreachEnforcementMessage",
          "title": "Power Breach Enforcement",
          "summary": "Infrastructure Management Agent publishes enforcement outcome.",
          "contentType": "application/cloudevents+json",
          "payload": {
            "description": "CloudEvents envelope for grid.powerbreach.enforcement.v1.",
            "allOf": [
              "$ref:$.channels.loadTargetSetChannel.messages.loadTargetSet.payload.allOf[0]",
              {
                "type": "object",
                "required": [
                  "data"
                ],
                "properties": {
                  "type": {
                    "const": "grid.powerbreach.enforcement.v1",
                    "x-parser-schema-id": "<anonymous-schema-66>"
                  },
                  "data": {
                    "description": "WIRE FORMAT: JWS compact serialization string (type: string).\nPLAINTEXT CONTENT: After JWS verification, the payload conforms\nto PowerBreachEnforcementData schema (type: object).\n",
                    "oneOf": [
                      "$ref:$.channels.loadTargetSetChannel.messages.loadTargetSet.payload.allOf[1].properties.data.oneOf[0]",
                      {
                        "type": "object",
                        "description": "Payload for grid.powerbreach.enforcement.v1.",
                        "required": [
                          "breach_id",
                          "feed_tag",
                          "action",
                          "code",
                          "results",
                          "timestamp"
                        ],
                        "properties": {
                          "breach_id": {
                            "type": "string",
                            "format": "uuid",
                            "description": "The breach_id from the originating grid.powerbreach.alert.v1 event.",
                            "x-parser-schema-id": "<anonymous-schema-68>"
                          },
                          "feed_tag": {
                            "type": "string",
                            "description": "The feed identifier this enforcement applies to.",
                            "x-parser-schema-id": "<anonymous-schema-69>"
                          },
                          "action": {
                            "type": "string",
                            "enum": [
                              "applied",
                              "reverted"
                            ],
                            "description": "What phase of enforcement this represents.",
                            "x-parser-schema-id": "<anonymous-schema-70>"
                          },
                          "code": {
                            "type": "string",
                            "enum": [
                              "success",
                              "partial",
                              "error"
                            ],
                            "description": "Overall enforcement outcome.",
                            "x-parser-schema-id": "<anonymous-schema-71>"
                          },
                          "diag_msg": {
                            "type": "string",
                            "description": "Human-readable diagnostic message.",
                            "x-parser-schema-id": "<anonymous-schema-72>"
                          },
                          "results": {
                            "type": "array",
                            "description": "Per-resource enforcement outcome.",
                            "items": {
                              "type": "object",
                              "description": "Per-resource enforcement outcome.",
                              "required": [
                                "resource_id",
                                "resource_type",
                                "requested_action",
                                "code"
                              ],
                              "properties": {
                                "resource_id": {
                                  "type": "string",
                                  "description": "The resource identifier from the shed hint.",
                                  "x-parser-schema-id": "<anonymous-schema-74>"
                                },
                                "resource_type": {
                                  "type": "string",
                                  "enum": [
                                    "node",
                                    "gpu",
                                    "chassis"
                                  ],
                                  "description": "Type of resource.",
                                  "x-parser-schema-id": "<anonymous-schema-75>"
                                },
                                "requested_action": {
                                  "type": "string",
                                  "enum": [
                                    "power_cap",
                                    "throttle",
                                    "suspend"
                                  ],
                                  "description": "The action that was requested.",
                                  "x-parser-schema-id": "<anonymous-schema-76>"
                                },
                                "code": {
                                  "type": "string",
                                  "enum": [
                                    "applied",
                                    "failed",
                                    "skipped"
                                  ],
                                  "description": "Per-resource enforcement status.",
                                  "x-parser-schema-id": "<anonymous-schema-77>"
                                },
                                "message": {
                                  "type": "string",
                                  "description": "Diagnostic message (e.g., \"BMC unreachable after 3 retries\").",
                                  "x-parser-schema-id": "<anonymous-schema-78>"
                                },
                                "applied_power": {
                                  "allOf": [
                                    "$ref:$.channels.powerStateStatusChannel.messages.powerStateStatus.payload.allOf[1].properties.data.oneOf[1].properties.feeds.additionalProperties.properties.metadata.properties.power_minimum.allOf[0]"
                                  ],
                                  "description": "The actual power cap applied, if different from the requested target_power.",
                                  "x-parser-schema-id": "<anonymous-schema-79>"
                                }
                              },
                              "x-parser-schema-id": "EnforcementResult"
                            },
                            "x-parser-schema-id": "<anonymous-schema-73>"
                          },
                          "timestamp": {
                            "type": "string",
                            "format": "date-time",
                            "description": "ISO 8601 UTC timestamp when enforcement completed.",
                            "x-parser-schema-id": "<anonymous-schema-80>"
                          }
                        },
                        "x-parser-schema-id": "PowerBreachEnforcementData"
                      }
                    ],
                    "x-parser-schema-id": "<anonymous-schema-67>"
                  }
                },
                "x-parser-schema-id": "<anonymous-schema-65>"
              }
            ],
            "x-parser-schema-id": "PowerBreachEnforcementEvent"
          },
          "x-parser-unique-object-id": "powerBreachEnforcement"
        }
      },
      "description": "Infrastructure Management Agent publishes enforcement outcomes after\napplying or reverting shed hints at the hardware level via Redfish BMC.\n",
      "bindings": {
        "mqtt": {
          "qos": 2
        }
      },
      "x-parser-unique-object-id": "powerBreachEnforcementChannel"
    }
  },
  "operations": {
    "publishLoadTargetSet": {
      "action": "send",
      "channel": "$ref:$.channels.loadTargetSetChannel",
      "summary": "ISV → Power Management Agent (set load target command)",
      "messages": [
        "$ref:$.channels.loadTargetSetChannel.messages.loadTargetSet"
      ],
      "x-parser-unique-object-id": "publishLoadTargetSet"
    },
    "subscribeLoadTargetSet": {
      "action": "receive",
      "channel": "$ref:$.channels.loadTargetSetChannel",
      "summary": "Power Management Agent subscribes to load target commands",
      "messages": [
        "$ref:$.channels.loadTargetSetChannel.messages.loadTargetSet"
      ],
      "x-parser-unique-object-id": "subscribeLoadTargetSet"
    },
    "publishPowerStateStatus": {
      "action": "send",
      "channel": "$ref:$.channels.powerStateStatusChannel",
      "summary": "Power Management Agent → ISVs (power state snapshot)",
      "messages": [
        "$ref:$.channels.powerStateStatusChannel.messages.powerStateStatus"
      ],
      "x-parser-unique-object-id": "publishPowerStateStatus"
    },
    "subscribePowerStateStatus": {
      "action": "receive",
      "channel": "$ref:$.channels.powerStateStatusChannel",
      "summary": "ISVs subscribe to power state status updates",
      "messages": [
        "$ref:$.channels.powerStateStatusChannel.messages.powerStateStatus"
      ],
      "x-parser-unique-object-id": "subscribePowerStateStatus"
    },
    "publishPowerBreachAlert": {
      "action": "send",
      "channel": "$ref:$.channels.powerBreachAlertChannel",
      "summary": "Power Management Agent → Infra Agent + ISVs (breach alert)",
      "messages": [
        "$ref:$.channels.powerBreachAlertChannel.messages.powerBreachAlert"
      ],
      "x-parser-unique-object-id": "publishPowerBreachAlert"
    },
    "subscribePowerBreachAlert": {
      "action": "receive",
      "channel": "$ref:$.channels.powerBreachAlertChannel",
      "summary": "Infra Agent and ISVs subscribe to breach alerts",
      "messages": [
        "$ref:$.channels.powerBreachAlertChannel.messages.powerBreachAlert"
      ],
      "x-parser-unique-object-id": "subscribePowerBreachAlert"
    },
    "publishPowerBreachEnforcement": {
      "action": "send",
      "channel": "$ref:$.channels.powerBreachEnforcementChannel",
      "summary": "Infra Agent → Power Management Agent + ISVs (enforcement outcome)",
      "messages": [
        "$ref:$.channels.powerBreachEnforcementChannel.messages.powerBreachEnforcement"
      ],
      "x-parser-unique-object-id": "publishPowerBreachEnforcement"
    },
    "subscribePowerBreachEnforcement": {
      "action": "receive",
      "channel": "$ref:$.channels.powerBreachEnforcementChannel",
      "summary": "Power Management Agent and ISVs subscribe to enforcement outcomes",
      "messages": [
        "$ref:$.channels.powerBreachEnforcementChannel.messages.powerBreachEnforcement"
      ],
      "x-parser-unique-object-id": "subscribePowerBreachEnforcement"
    }
  },
  "components": {
    "schemas": {
      "PowerUnit": "$ref:$.channels.powerStateStatusChannel.messages.powerStateStatus.payload.allOf[1].properties.data.oneOf[1].properties.feeds.additionalProperties.properties.metadata.properties.power_minimum.allOf[0]",
      "Interval": "$ref:$.channels.loadTargetSetChannel.messages.loadTargetSet.payload.allOf[1].properties.data.oneOf[1].properties.targets.items.properties.interval",
      "Strategy": "$ref:$.channels.loadTargetSetChannel.messages.loadTargetSet.payload.allOf[1].properties.data.oneOf[1].properties.targets.items.properties.strategy",
      "LoadConstraint": "$ref:$.channels.loadTargetSetChannel.messages.loadTargetSet.payload.allOf[1].properties.data.oneOf[1].properties.targets.items.properties.load_constraint.oneOf[0]",
      "FeedTags": "$ref:$.channels.loadTargetSetChannel.messages.loadTargetSet.payload.allOf[1].properties.data.oneOf[1].properties.targets.items.properties.feed_tags",
      "ShedHint": "$ref:$.channels.powerBreachAlertChannel.messages.powerBreachAlert.payload.allOf[1].properties.data.oneOf[1].properties.shed_hints.items",
      "EnforcementResult": "$ref:$.channels.powerBreachEnforcementChannel.messages.powerBreachEnforcement.payload.allOf[1].properties.data.oneOf[1].properties.results.items",
      "FeedMetadata": "$ref:$.channels.powerStateStatusChannel.messages.powerStateStatus.payload.allOf[1].properties.data.oneOf[1].properties.feeds.additionalProperties.properties.metadata",
      "FeedTarget": "$ref:$.channels.powerStateStatusChannel.messages.powerStateStatus.payload.allOf[1].properties.data.oneOf[1].properties.feeds.additionalProperties.properties.targets.items",
      "FeedState": "$ref:$.channels.powerStateStatusChannel.messages.powerStateStatus.payload.allOf[1].properties.data.oneOf[1].properties.feeds.additionalProperties",
      "BreachDetails": "$ref:$.channels.powerBreachAlertChannel.messages.powerBreachAlert.payload.allOf[1].properties.data.oneOf[1].properties.breach",
      "BreachTarget": "$ref:$.channels.powerBreachAlertChannel.messages.powerBreachAlert.payload.allOf[1].properties.data.oneOf[1].properties.target",
      "SetLoadTargetData": "$ref:$.channels.loadTargetSetChannel.messages.loadTargetSet.payload.allOf[1].properties.data.oneOf[1]",
      "PowerStateStatusData": "$ref:$.channels.powerStateStatusChannel.messages.powerStateStatus.payload.allOf[1].properties.data.oneOf[1]",
      "PowerBreachAlertData": "$ref:$.channels.powerBreachAlertChannel.messages.powerBreachAlert.payload.allOf[1].properties.data.oneOf[1]",
      "PowerBreachEnforcementData": "$ref:$.channels.powerBreachEnforcementChannel.messages.powerBreachEnforcement.payload.allOf[1].properties.data.oneOf[1]",
      "JwsCompactSerialization": "$ref:$.channels.loadTargetSetChannel.messages.loadTargetSet.payload.allOf[1].properties.data.oneOf[0]",
      "BaseCloudEvent": "$ref:$.channels.loadTargetSetChannel.messages.loadTargetSet.payload.allOf[0]",
      "LoadTargetSetEvent": "$ref:$.channels.loadTargetSetChannel.messages.loadTargetSet.payload",
      "PowerStateStatusEvent": "$ref:$.channels.powerStateStatusChannel.messages.powerStateStatus.payload",
      "PowerBreachAlertEvent": "$ref:$.channels.powerBreachAlertChannel.messages.powerBreachAlert.payload",
      "PowerBreachEnforcementEvent": "$ref:$.channels.powerBreachEnforcementChannel.messages.powerBreachEnforcement.payload"
    },
    "messages": {
      "LoadTargetSetMessage": "$ref:$.channels.loadTargetSetChannel.messages.loadTargetSet",
      "PowerStateStatusMessage": "$ref:$.channels.powerStateStatusChannel.messages.powerStateStatus",
      "PowerBreachAlertMessage": "$ref:$.channels.powerBreachAlertChannel.messages.powerBreachAlert",
      "PowerBreachEnforcementMessage": "$ref:$.channels.powerBreachEnforcementChannel.messages.powerBreachEnforcement"
    },
    "parameters": {
      "identifier": "$ref:$.channels.loadTargetSetChannel.parameters.identifier",
      "infraAgentId": "$ref:$.channels.powerBreachEnforcementChannel.parameters.infraAgentId"
    },
    "securitySchemes": {
      "oauth2Ssa": {
        "type": "oauth2",
        "description": "OAuth2 token in MQTT password field (username \"oauth2token\").",
        "flows": {
          "clientCredentials": {
            "tokenUrl": "https://auth.nvidia.com/token",
            "availableScopes": {
              "mqtt": "Access to MQTT topics"
            }
          }
        }
      },
      "mTLS": {
        "type": "X509",
        "description": "Mutual TLS with client certificate signed by cluster CA."
      }
    }
  },
  "x-parser-spec-parsed": true,
  "x-parser-api-version": 3,
  "x-parser-spec-stringified": true
};
    const config = {"show":{"sidebar":true},"sidebar":{"showOperations":"byDefault"}};
    const appRoot = document.getElementById('root');
    AsyncApiStandalone.render(
        { schema, config, }, appRoot
    );
  