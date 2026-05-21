
    const schema = {
  "asyncapi": "3.1.0",
  "info": {
    "title": "BMS Event Bus",
    "version": "2.1.0",
    "description": "Telemetry and control event catalog for the Building Management System (BMS)\nover MQTT. Provides real-time point values and point metadata for all supported\nobject and point types.\n\n## How to use this spec\n\nEach monitored point follows a **Value / Metadata** pattern:\n- **Value** messages carry the live reading (`value`, `timestamp`, `quality`).\n  Subscribe to a value channel to receive real-time telemetry.\n  Values are published whenever they change and republished every 100 seconds\n  when they do not change.\n- **Metadata** messages describe the point (units, identifiers, relationships).\n  **Always receive metadata before interpreting values.** Metadata is retained and published\n  once at startup. It is subsequently published every 100 seconds.\n\n## Topic structure\n\n| Publisher   | Topic Type | Pattern                                                               |\n|-------------|------------|-----------------------------------------------------------------------|\n| BMS         | Value      | `BMS/v1/PUB/Value/{objectType}/{pointType}/{tagPath}`                |\n| BMS         | Metadata   | `BMS/v1/PUB/Metadata/{objectType}/{pointType}/{tagPath}`             |\n| Integration | Value      | `BMS/v1/{integration}/Value/{objectType}/{pointType}/{tagPath}`      |\n\nThe `{tagPath}` is a vendor-defined hierarchical path and may contain multiple\n`/` segments. Each `{tagPath}` must be unique for each point and is usually derived from the BMS system based on the BMS point name.\n\nUse `#` to subscribe to all topics under a given hierarchy (multi-level wildcard).\n\nUse `+` to match exactly one topic level (single-level wildcard).\n\n## Publisher rules\n\n- **BMS** publishes all metadata — including for points whose values are\n  written by integrations.\n- **BMS** publishes its own point values on `BMS/v1/PUB/Value/...`.\n- **Integrations** are any system (MQTT Client) external to the BMS. Whenever they need to directly send messages to the BMS, they publish values on `BMS/v1/{integration}/Value/...`.\n Integrations do **not** publish metadata.\n\n\n## Integration publishing contract\n\n> **This rule applies globally to every integration-published point across all\n> objectTypes.**\n\nBMS publishes metadata for every monitored point — including points whose\n_values_ are written by external integrations. For such points the metadata\npayload contains an `integration` field identifying which integration owns\nthat point's value.\n\n| Metadata field | Type | Meaning |\n|----------------|------|---------|\n| `integration` | string | Identifier of the integration that must publish the value for this point |\n\n### Topic derivation rule\n\nThe value topic is derived **methodically** from the respective metadata topic (published by BMS) that\ncarried the `integration` field. DSX Exchange Access Control Lists will\ntypically be created so that MQTT Clients are provided access to publish and\nsubscribe to specific namespaces that align with the `[Publisher]`:\n\n BMS/v1/**[Publisher]/[TopicType]**/{objectType}/{pointType}/{tagPath}\n\n| Segment | Metadata topic | Value topic |\n|---------|---------------|-------------|\n| Publisher | `PUB` | value of `integration` field in metadata |\n| TopicType | `Metadata` | `Value` |\n| Remainder | `{objectType}/{pointType}/{tagPath}` | `{objectType}/{pointType}/{tagPath}` |\n\nIn other words, the Publisher integration will derive the following value topic to publish based on the corresponding BMS metadata:\n\n    BMS/v1/PUB/Metadata/{objectType}/{pointType}/{tagPath}\n            ↓  replace PUB → {integration},  Metadata → Value\n    BMS/v1/{integration}/Value/{objectType}/{pointType}/{tagPath}\n\n### Contract\n\nWhen an integration receives a BMS metadata message and the `integration`\nfield **matches its own identifier**, the integration **MUST**:\n\n1. Note the full metadata topic it arrived on.\n2. Derive the value topic by substituting `PUB` → own identifier and\n   `Metadata` → `Value`, keeping `{objectType}/{pointType}/{tagPath}`\n   unchanged.\n3. Publish value messages to that derived topic.\n\nIntegrations **MUST NOT**:\n\n- Publish values for points whose metadata `integration` field does not\n  match their own identifier.\n- Publish metadata (BMS is the sole metadata publisher).\n\n### Example flow\n\n    BMS publishes metadata →\n      Topic:   BMS/v1/PUB/Metadata/CDU/LiquidTemperatureSpRequest/site1/row3/cdu5\n      Payload: { ..., \"integration\": \"MEPAI\", ... }\n\n    Integration \"MEPAI\" receives the metadata, recognises its identifier,\n    derives its value topic →\n      BMS/v1/PUB/Metadata/CDU/LiquidTemperatureSpRequest/site1/row3/cdu5\n                ↓ PUB→MEPAI, Metadata→Value\n      BMS/v1/MEPAI/Value/CDU/LiquidTemperatureSpRequest/site1/row3/cdu5\n\n    MEPAI publishes its setpoint reading to that derived topic.\n\n  The same contract governs all other integration-published points, including\n  `Rack/RackLiquidIsolationRequest`, `Rack/RackElectricalIsolationRequest`,\n  `System/HeartbeatEchoBms`, `System/HearbeatTimestampIntegration`, `System/HeartbeatEchoIntegration`,\n  `CDU/LiquidTemperatureSpRequest`, and any future integration-owned pointTypes.\n  Note that for the `System/HeartbeatEchoBms` point, although the BMS publishes\n  integration metadata, the BMS writes to the value and not the integration.\n\n\n## Metadata types and concepts\n\n- **objectType**: Object Types are restricted to specific strings in accordance\n  with this AsyncAPI. They typically represent BMS equipment or devices.\n\n  - **System**: A System can be the BMS or an Integration to the BMS.\n    Heartbeat points and system-to-system communication point types are\n    typically defined inside of a System object type. System Heartbeat point\n    types are expected to operate as follows. An integration may choose to use\n    the Echo points or not:\n\n    - **HeartbeatTimestampBms** — The BMS will publish a timestamp every 10 seconds.\n    - **HearbeatTimestampIntegration** — The Integration will publish a timestamp every 10 seconds.\n    - **HeartbeatEchoBms** — The BMS will publish an echo of the integration\n      timestamp (reads `HearbeatTimestampIntegration` value and re-publishes\n      the same value on this point type).\n    - **HeartbeatEchoIntegration** — The Integration will publish an echo of\n      the BMS timestamp (reads `HeartbeatEchoBms` value and re-publishes the\n      same value on this point type).\n\n  - **Rack**: A Rack is a special object type and has specific point types that\n    can only be used with a Rack object type. Many integrations and MQTT Clients\n    will only ingest data from the Rack object type. Other integrations will\n    consider Rack data as the most important or interesting data in the AI\n    Factory. Mechanical and Electrical design (and therefore BMS Data) at the\n    rack is also more standardized than mechanical and electrical systems as you\n    move out of the white space and to the gray space of the AI Factory. For\n    these reasons, the point types associated with a Rack object type are\n    generally more specific than any other object type, making ingesting and\n    understanding rack data more straight-forward.\n\n  - **PowerMeter**: A PowerMeter is a special object type and has specific point\n    types that can only be used with a PowerMeter object type. PowerMeter point\n    types contain the metadata needed to understand electrical power path. This\n    data is used by integrations / MQTT Clients for power management strategies.\n\n  - **Electrical Equipment**: This is a general category, and several object\n    types exist in this AsyncAPI for electrical equipment. Electrical equipment\n    point types typically use metadata to associate the object / equipment with\n    a power meter to show where the equipment lands in the power path.\n\n  - **Mechanical Equipment**: This is a general category, and several object\n    types exist in this AsyncAPI for mechanical equipment.\n\n  - **GenericObject**: This object type is reserved and should not be used\n    unless no other object type is applicable.\n\n- **pointType**: Point Types are restricted to specific strings in accordance\n  with this AsyncAPI. Point types are also restricted to specific object types.\n  Some point types apply to multiple object types while others are restricted\n  to specific object types.\n\n- **engUnit**: Engineering Units describe the units of measure for the point /\n  topic. If engineering units are used, then state text does not apply.\n\n- **stateText**: State Text describes what a binary or integer value means.\n  Each binary or integer value will have a state text identified. If state text\n  is used, then engineering units do not apply.\n\n- **rackLocationName**: This is the human readable name given to a rack location.\n\n- **rackLocationId**: This is the unique identifier for a rack location. It can\n  be the same as the rackLocationName if human readable and unique. This must\n  allow the IT side integrations and OT side BMS to associate a point with the\n  same physical rack (same identifier on both systems).\n\n- **objectName**: This is the human readable name given to the object.\n\n- **objectId**: This is the unique identifier for an object. It can be the same\n  as the objectName if human readable and unique.\n\n- **associateId**: This will list the objectId of an associated object. The\n  intent is for this object to be considered part of the other object, especially\n  for \"servesId\" metadata. Commonly used to prevent parallel power paths\n  (parallel \"serves\" relationships) when electrical equipment objects need to be\n  associated with power meters. Also applicable to mechanical equipment object\n  types to prevent liquid and air flow parallel paths where not intended.\n\n- **servesId**: This will list the objectId of the object that is served by\n  this object. Creates a one-way relationship. Typically used to indicate\n  electrical power path or liquid/air fluid flow path towards the rack.\n\n- **processArea**: This is used to provide more information on the point\n  location or purpose. In general, process area metadata should be used, in\n  conjunction with other metadata, to make points unique and allow integrations\n  / MQTT Clients to clearly understand what a point represents in the AI\n  factory. Example: for a CDU object type and LiquidTemperature point type, the\n  process area metadata could include \"Secondary\" and \"Supply\". This would make\n  it clear that the temperature sensor was located on the secondary side of the\n  CDU and on the supply line.\n\n- **phase**: This is used to identify which phase, of a 3-phase electrical\n  system, the point is associated with.\n\n- **isSetpoint**: This can be shown as \"True\" to indicate a point is a setpoint\n  as opposed to a sensor reading. A setpoint is a target or ideal value that a\n  system is trying to maintain a process variable at.\n\n- **scope**: Scope is used for System heartbeat points. If the BMS has multiple\n  MQTT Clients connected to DSX Exchange, each could be publishing different\n  data to the MQTT Broker. In this case each MQTT Client should have separate\n  heartbeat points. Scope can be used in this case to identify what MQTT Topics\n  / Namespace the heartbeat point is associated with — and thereby what topics\n  are impacted when a heartbeat is lost.\n\n- **integration**: Integration metadata is used to indicate which integration a\n  point is associated with. It also indicates the namespace `[Publisher]` an\n  integration is required to write the corresponding value back to.\n"
  },
  "channels": {
    "rackBmsValue": {
      "address": "BMS/v1/PUB/Value/Rack/{pointType}/{tagPath}",
      "description": "Real-time values published by the BMS for all Rack monitoring points.\n\n**MQTT wildcard examples**\n- All rack values: `BMS/v1/PUB/Value/Rack/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "RackLiquidSupplyTemperature",
            "RackLiquidReturnTemperature",
            "RackLiquidFlow",
            "RackLiquidDifferentialPressure",
            "RackControlValvePosition",
            "RackPower",
            "RackLeakDetect",
            "RackLeakSensorFault",
            "RackLeakDetectTray",
            "RackLiquidIsolationStatus",
            "RackElectricalIsolationStatus"
          ],
          "description": "BMS-published Rack point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "valueMessage": {
          "name": "ValueMessage",
          "title": "Point Value",
          "description": "Live value message. Payload envelope is identical for all point types.\nThe semantic meaning of `value` is determined by the corresponding\nmetadata message.\n",
          "payload": {
            "type": "object",
            "required": [
              "value",
              "timestamp",
              "quality"
            ],
            "properties": {
              "value": {
                "oneOf": [
                  {
                    "type": "number",
                    "x-parser-schema-id": "<anonymous-schema-5>"
                  },
                  {
                    "type": "null",
                    "x-parser-schema-id": "<anonymous-schema-6>"
                  }
                ],
                "description": "Live reading for the point (float). May be null when the BMS has no valid reading available.\n",
                "examples": [
                  22.96215,
                  0.239,
                  null,
                  1
                ],
                "x-parser-schema-id": "<anonymous-schema-4>"
              },
              "timestamp": {
                "type": "integer",
                "description": "Unix timestamp in epoch milliseconds (source event time).",
                "examples": [
                  1743620423000
                ],
                "x-parser-schema-id": "<anonymous-schema-7>"
              },
              "quality": {
                "type": "integer",
                "description": "`1` = good quality. Any other integer indicates value is not trustworthy\n",
                "examples": [
                  1,
                  0
                ],
                "x-parser-schema-id": "<anonymous-schema-8>"
              }
            },
            "additionalProperties": false,
            "x-parser-schema-id": "<anonymous-schema-3>"
          },
          "x-parser-unique-object-id": "valueMessage"
        }
      },
      "x-parser-unique-object-id": "rackBmsValue"
    },
    "rackMetadata": {
      "address": "BMS/v1/PUB/Metadata/Rack/{pointType}/{tagPath}",
      "description": "BMS-published metadata for all Rack points.\nIncludes integration-owned points which carries the `integration` field.\n\n**MQTT wildcard examples**\n- All rack metadata: `BMS/v1/PUB/Metadata/Rack/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "RackLiquidSupplyTemperature",
            "RackLiquidReturnTemperature",
            "RackLiquidFlow",
            "RackLiquidDifferentialPressure",
            "RackLiquidDifferentialPressureSp",
            "RackControlValvePosition",
            "RackPower",
            "RackLeakDetect",
            "RackLeakSensorFault",
            "RackLeakDetectTray",
            "RackLiquidIsolationStatus",
            "RackElectricalIsolationStatus",
            "RackLiquidIsolationRequest",
            "RackElectricalIsolationRequest"
          ],
          "description": "Rack point type."
        },
        "tagPath": {
          "description": "Must match the tagPath of the corresponding value topic exactly."
        }
      },
      "messages": {
        "rackLiquidSupplyTemperature": {
          "payload": {
            "allOf": [
              {
                "description": "Composed base for all BMS-published Rack metadata messages. Combines MetadataBase with rackLocationIdentifiers.\n",
                "allOf": [
                  {
                    "type": "object",
                    "description": "Minimum fields present on every metadata message.",
                    "required": [
                      "objectType",
                      "pointType"
                    ],
                    "properties": {
                      "objectType": {
                        "type": "string",
                        "description": "Canonical object type. Matches the objectType MQTT topic segment.",
                        "x-parser-schema-id": "<anonymous-schema-11>"
                      },
                      "pointType": {
                        "type": "string",
                        "description": "Canonical point type. Matches the pointType MQTT topic segment.",
                        "x-parser-schema-id": "<anonymous-schema-12>"
                      }
                    },
                    "x-parser-schema-id": "MetadataBase"
                  },
                  {
                    "type": "object",
                    "description": "Rack-specific identifier fields added to all Rack metadata messages.",
                    "required": [
                      "rackLocationName",
                      "rackLocationId"
                    ],
                    "properties": {
                      "rackLocationName": {
                        "type": "string",
                        "description": "Human-readable rack name as defined by the BMS.",
                        "x-parser-schema-id": "<anonymous-schema-13>"
                      },
                      "rackLocationId": {
                        "type": "string",
                        "description": "Stable unique identifier for the rack.",
                        "x-parser-schema-id": "<anonymous-schema-14>"
                      }
                    },
                    "x-parser-schema-id": "rackLocationIdentifiers"
                  }
                ],
                "x-parser-schema-id": "RackMetadataBase"
              },
              {
                "type": "object",
                "required": [
                  "engUnit"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "RackLiquidSupplyTemperature"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-15>"
                  },
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "Rack"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-16>"
                  },
                  "engUnit": {
                    "type": "string",
                    "enum": [
                      "C"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-17>"
                  }
                },
                "x-parser-schema-id": "RackLiquidSupplyTemperatureFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "RackLiquidSupplyTemperatureMetadata"
          },
          "x-parser-unique-object-id": "rackLiquidSupplyTemperature",
          "x-parser-message-name": "RackLiquidSupplyTemperatureMsg"
        },
        "rackLiquidReturnTemperature": {
          "payload": {
            "allOf": [
              "$ref:$.channels.rackMetadata.messages.rackLiquidSupplyTemperature.payload.allOf[0]",
              {
                "type": "object",
                "required": [
                  "engUnit"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "RackLiquidReturnTemperature"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-18>"
                  },
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "Rack"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-19>"
                  },
                  "engUnit": {
                    "type": "string",
                    "enum": [
                      "C"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-20>"
                  }
                },
                "x-parser-schema-id": "RackLiquidReturnTemperatureFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "RackLiquidReturnTemperatureMetadata"
          },
          "x-parser-unique-object-id": "rackLiquidReturnTemperature",
          "x-parser-message-name": "RackLiquidReturnTemperatureMsg"
        },
        "rackLiquidFlow": {
          "payload": {
            "allOf": [
              "$ref:$.channels.rackMetadata.messages.rackLiquidSupplyTemperature.payload.allOf[0]",
              {
                "type": "object",
                "required": [
                  "engUnit"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "RackLiquidFlow"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-21>"
                  },
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "Rack"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-22>"
                  },
                  "engUnit": {
                    "type": "string",
                    "enum": [
                      "LPM"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-23>"
                  }
                },
                "x-parser-schema-id": "RackLiquidFlowFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "RackLiquidFlowMetadata"
          },
          "x-parser-unique-object-id": "rackLiquidFlow",
          "x-parser-message-name": "RackLiquidFlowMsg"
        },
        "rackLiquidDifferentialPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.rackMetadata.messages.rackLiquidSupplyTemperature.payload.allOf[0]",
              {
                "type": "object",
                "required": [
                  "engUnit"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "RackLiquidDifferentialPressure"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-24>"
                  },
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "Rack"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-25>"
                  },
                  "engUnit": {
                    "type": "string",
                    "enum": [
                      "kPa"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-26>"
                  }
                },
                "x-parser-schema-id": "RackLiquidDifferentialPressureFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "RackLiquidDifferentialPressureMetadata"
          },
          "x-parser-unique-object-id": "rackLiquidDifferentialPressure",
          "x-parser-message-name": "RackLiquidDifferentialPressureMsg"
        },
        "rackLiquidDifferentialPressureSp": {
          "payload": {
            "allOf": [
              "$ref:$.channels.rackMetadata.messages.rackLiquidSupplyTemperature.payload.allOf[0]",
              {
                "type": "object",
                "required": [
                  "engUnit"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "RackLiquidDifferentialPressureSp"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-27>"
                  },
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "Rack"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-28>"
                  },
                  "engUnit": {
                    "type": "string",
                    "enum": [
                      "kPa"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-29>"
                  },
                  "isSetpoint": {
                    "type": "boolean",
                    "description": "Optional inclusion. Indicates this point is a target value written to control equipment behavior.\n",
                    "x-parser-schema-id": "<anonymous-schema-30>"
                  }
                },
                "x-parser-schema-id": "RackLiquidDifferentialPressureSpFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "RackLiquidDifferentialPressureSpMetadata"
          },
          "x-parser-unique-object-id": "rackLiquidDifferentialPressureSp",
          "x-parser-message-name": "RackLiquidDifferentialPressureSpMsg"
        },
        "rackControlValvePosition": {
          "payload": {
            "allOf": [
              "$ref:$.channels.rackMetadata.messages.rackLiquidSupplyTemperature.payload.allOf[0]",
              {
                "type": "object",
                "required": [
                  "engUnit"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "RackControlValvePosition"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-31>"
                  },
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "Rack"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-32>"
                  },
                  "engUnit": {
                    "type": "string",
                    "enum": [
                      "%"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-33>"
                  }
                },
                "x-parser-schema-id": "RackControlValvePositionFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "RackControlValvePositionMetadata"
          },
          "x-parser-unique-object-id": "rackControlValvePosition",
          "x-parser-message-name": "RackControlValvePositionMsg"
        },
        "rackPower": {
          "payload": {
            "allOf": [
              "$ref:$.channels.rackMetadata.messages.rackLiquidSupplyTemperature.payload.allOf[0]",
              {
                "type": "object",
                "required": [
                  "engUnit"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "RackPower"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-34>"
                  },
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "Rack"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-35>"
                  },
                  "engUnit": {
                    "type": "string",
                    "enum": [
                      "kW"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-36>"
                  }
                },
                "x-parser-schema-id": "RackPowerFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "RackPowerMetadata"
          },
          "x-parser-unique-object-id": "rackPower",
          "x-parser-message-name": "RackPowerMsg"
        },
        "rackLeakDetect": {
          "payload": {
            "allOf": [
              "$ref:$.channels.rackMetadata.messages.rackLiquidSupplyTemperature.payload.allOf[0]",
              {
                "type": "object",
                "description": "0 = No Leak, 1 = Leak. No engUnit.",
                "required": [
                  "stateText"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "RackLeakDetect"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-37>"
                  },
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "Rack"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-38>"
                  },
                  "stateText": {
                    "type": "array",
                    "description": "State label mapping for leak detection.\n",
                    "items": {
                      "type": "object",
                      "required": [
                        "value",
                        "text"
                      ],
                      "properties": {
                        "value": {
                          "type": "integer",
                          "description": "Numeric state value.",
                          "x-parser-schema-id": "<anonymous-schema-41>"
                        },
                        "text": {
                          "type": "string",
                          "description": "Human-readable label for this state.",
                          "x-parser-schema-id": "<anonymous-schema-42>"
                        }
                      },
                      "additionalProperties": false,
                      "x-parser-schema-id": "<anonymous-schema-40>"
                    },
                    "examples": [
                      [
                        {
                          "value": 0,
                          "text": "NoLeak"
                        },
                        {
                          "value": 1,
                          "text": "Leak"
                        }
                      ]
                    ],
                    "x-parser-schema-id": "<anonymous-schema-39>"
                  }
                },
                "x-parser-schema-id": "RackLeakDetectFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "RackLeakDetectMetadata"
          },
          "x-parser-unique-object-id": "rackLeakDetect",
          "x-parser-message-name": "RackLeakDetectMsg"
        },
        "rackLeakSensorFault": {
          "payload": {
            "allOf": [
              "$ref:$.channels.rackMetadata.messages.rackLiquidSupplyTemperature.payload.allOf[0]",
              {
                "type": "object",
                "description": "0 = No Fault, 1 = Fault. No engUnit.",
                "required": [
                  "stateText"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "RackLeakSensorFault"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-43>"
                  },
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "Rack"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-44>"
                  },
                  "stateText": {
                    "type": "array",
                    "description": "State label mapping for leak sensor fault status.\n",
                    "items": {
                      "type": "object",
                      "required": [
                        "value",
                        "text"
                      ],
                      "properties": {
                        "value": {
                          "type": "integer",
                          "description": "Numeric state value.",
                          "x-parser-schema-id": "<anonymous-schema-47>"
                        },
                        "text": {
                          "type": "string",
                          "description": "Human-readable label for this state.",
                          "x-parser-schema-id": "<anonymous-schema-48>"
                        }
                      },
                      "additionalProperties": false,
                      "x-parser-schema-id": "<anonymous-schema-46>"
                    },
                    "examples": [
                      [
                        {
                          "value": 0,
                          "text": "NoFault"
                        },
                        {
                          "value": 1,
                          "text": "Fault"
                        }
                      ]
                    ],
                    "x-parser-schema-id": "<anonymous-schema-45>"
                  }
                },
                "x-parser-schema-id": "RackLeakSensorFaultFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "RackLeakSensorFaultMetadata"
          },
          "x-parser-unique-object-id": "rackLeakSensorFault",
          "x-parser-message-name": "RackLeakSensorFaultMsg"
        },
        "rackLeakDetectTray": {
          "payload": {
            "allOf": [
              "$ref:$.channels.rackMetadata.messages.rackLiquidSupplyTemperature.payload.allOf[0]",
              {
                "type": "object",
                "description": "0 = No Leak, 1 = Leak (tray sensor). No engUnit. Integration-published value.",
                "required": [
                  "stateText"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "RackLeakDetectTray"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-49>"
                  },
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "Rack"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-50>"
                  },
                  "stateText": {
                    "type": "array",
                    "description": "State label mapping for tray leak detection.\n",
                    "items": {
                      "type": "object",
                      "required": [
                        "value",
                        "text"
                      ],
                      "properties": {
                        "value": {
                          "type": "integer",
                          "description": "Numeric state value.",
                          "x-parser-schema-id": "<anonymous-schema-53>"
                        },
                        "text": {
                          "type": "string",
                          "description": "Human-readable label for this state.",
                          "x-parser-schema-id": "<anonymous-schema-54>"
                        }
                      },
                      "additionalProperties": false,
                      "x-parser-schema-id": "<anonymous-schema-52>"
                    },
                    "examples": [
                      [
                        {
                          "value": 0,
                          "text": "NoLeak"
                        },
                        {
                          "value": 1,
                          "text": "Leak"
                        }
                      ]
                    ],
                    "x-parser-schema-id": "<anonymous-schema-51>"
                  }
                },
                "x-parser-schema-id": "RackLeakDetectTrayFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "RackLeakDetectTrayMetadata"
          },
          "x-parser-unique-object-id": "rackLeakDetectTray",
          "x-parser-message-name": "RackLeakDetectTrayMsg"
        },
        "rackLiquidIsolationStatus": {
          "payload": {
            "allOf": [
              "$ref:$.channels.rackMetadata.messages.rackLiquidSupplyTemperature.payload.allOf[0]",
              {
                "type": "object",
                "description": "0=NotIsolated, 1=Isolated. No engUnit.",
                "required": [
                  "stateText"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "RackLiquidIsolationStatus"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-55>"
                  },
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "Rack"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-56>"
                  },
                  "stateText": {
                    "type": "array",
                    "description": "State label mapping for liquid isolation status.\n",
                    "items": {
                      "type": "object",
                      "required": [
                        "value",
                        "text"
                      ],
                      "properties": {
                        "value": {
                          "type": "integer",
                          "description": "Numeric state value.",
                          "x-parser-schema-id": "<anonymous-schema-59>"
                        },
                        "text": {
                          "type": "string",
                          "description": "Human-readable label for this state.",
                          "x-parser-schema-id": "<anonymous-schema-60>"
                        }
                      },
                      "additionalProperties": false,
                      "x-parser-schema-id": "<anonymous-schema-58>"
                    },
                    "examples": [
                      [
                        {
                          "value": 0,
                          "text": "NotIsolated"
                        },
                        {
                          "value": 1,
                          "text": "Isolated"
                        }
                      ]
                    ],
                    "x-parser-schema-id": "<anonymous-schema-57>"
                  }
                },
                "x-parser-schema-id": "RackLiquidIsolationStatusFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "RackLiquidIsolationStatusMetadata"
          },
          "x-parser-unique-object-id": "rackLiquidIsolationStatus",
          "x-parser-message-name": "RackLiquidIsolationStatusMsg"
        },
        "rackElectricalIsolationStatus": {
          "payload": {
            "allOf": [
              "$ref:$.channels.rackMetadata.messages.rackLiquidSupplyTemperature.payload.allOf[0]",
              {
                "type": "object",
                "description": "0 = NotIsolated, 1 = Isolated. No engUnit.",
                "required": [
                  "stateText"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "RackElectricalIsolationStatus"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-61>"
                  },
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "Rack"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-62>"
                  },
                  "stateText": {
                    "type": "array",
                    "description": "State label mapping for electrical isolation status.\n",
                    "items": {
                      "type": "object",
                      "required": [
                        "value",
                        "text"
                      ],
                      "properties": {
                        "value": {
                          "type": "integer",
                          "description": "Numeric state value.",
                          "x-parser-schema-id": "<anonymous-schema-65>"
                        },
                        "text": {
                          "type": "string",
                          "description": "Human-readable label for this state.",
                          "x-parser-schema-id": "<anonymous-schema-66>"
                        }
                      },
                      "additionalProperties": false,
                      "x-parser-schema-id": "<anonymous-schema-64>"
                    },
                    "examples": [
                      [
                        {
                          "value": 0,
                          "text": "NotIsolated"
                        },
                        {
                          "value": 1,
                          "text": "Isolated"
                        }
                      ]
                    ],
                    "x-parser-schema-id": "<anonymous-schema-63>"
                  }
                },
                "x-parser-schema-id": "RackElectricalIsolationStatusFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "RackElectricalIsolationStatusMetadata"
          },
          "x-parser-unique-object-id": "rackElectricalIsolationStatus",
          "x-parser-message-name": "RackElectricalIsolationStatusMsg"
        },
        "rackLiquidIsolationRequest": {
          "payload": {
            "allOf": [
              {
                "description": "Composed base for Integration-published Rack metadata messages. Extends RackMetadataBase with IntegrationPublisherFields.\n",
                "allOf": [
                  "$ref:$.channels.rackMetadata.messages.rackLiquidSupplyTemperature.payload.allOf[0]",
                  {
                    "type": "object",
                    "description": "Fields added to metadata for integration-published points. Integrations MUST publish values to the exact topic corresponding to `integration` — do not construct it independently.\n",
                    "required": [
                      "integration"
                    ],
                    "properties": {
                      "integration": {
                        "type": "string",
                        "description": "Integration identifier responsible for publishing this value.",
                        "x-parser-schema-id": "<anonymous-schema-67>"
                      }
                    },
                    "x-parser-schema-id": "IntegrationPublisherFields"
                  }
                ],
                "x-parser-schema-id": "RackIntegrationMetadataBase"
              },
              {
                "type": "object",
                "description": "0 = Not Requested, 1 = Requested, -1 = Unknown. Integration-published value.",
                "required": [
                  "stateText"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "RackLiquidIsolationRequest"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-68>"
                  },
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "Rack"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-69>"
                  },
                  "stateText": {
                    "type": "array",
                    "description": "State label mapping for liquid isolation request.\n",
                    "items": {
                      "type": "object",
                      "required": [
                        "value",
                        "text"
                      ],
                      "properties": {
                        "value": {
                          "type": "integer",
                          "description": "Numeric state value.",
                          "x-parser-schema-id": "<anonymous-schema-72>"
                        },
                        "text": {
                          "type": "string",
                          "description": "Human-readable label for this state.",
                          "x-parser-schema-id": "<anonymous-schema-73>"
                        }
                      },
                      "additionalProperties": false,
                      "x-parser-schema-id": "<anonymous-schema-71>"
                    },
                    "examples": [
                      [
                        {
                          "value": 0,
                          "text": "NoIsolationRequested"
                        },
                        {
                          "value": 1,
                          "text": "IsolationRequested"
                        }
                      ]
                    ],
                    "x-parser-schema-id": "<anonymous-schema-70>"
                  }
                },
                "x-parser-schema-id": "RackLiquidIsolationRequestFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "RackLiquidIsolationRequestMetadata"
          },
          "x-parser-unique-object-id": "rackLiquidIsolationRequest",
          "x-parser-message-name": "RackLiquidIsolationRequestMsg"
        },
        "rackElectricalIsolationRequest": {
          "payload": {
            "allOf": [
              "$ref:$.channels.rackMetadata.messages.rackLiquidIsolationRequest.payload.allOf[0]",
              {
                "type": "object",
                "description": "0 = Not Requested, 1 = Requested. Integration-published value.",
                "required": [
                  "stateText"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "RackElectricalIsolationRequest"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-74>"
                  },
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "Rack"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-75>"
                  },
                  "stateText": {
                    "type": "array",
                    "description": "State label mapping for electrical isolation request.\n",
                    "items": {
                      "type": "object",
                      "required": [
                        "value",
                        "text"
                      ],
                      "properties": {
                        "value": {
                          "type": "integer",
                          "description": "Numeric state value.",
                          "x-parser-schema-id": "<anonymous-schema-78>"
                        },
                        "text": {
                          "type": "string",
                          "description": "Human-readable label for this state.",
                          "x-parser-schema-id": "<anonymous-schema-79>"
                        }
                      },
                      "additionalProperties": false,
                      "x-parser-schema-id": "<anonymous-schema-77>"
                    },
                    "examples": [
                      [
                        {
                          "value": 0,
                          "text": "NoIsolationRequested"
                        },
                        {
                          "value": 1,
                          "text": "IsolationRequested"
                        }
                      ]
                    ],
                    "x-parser-schema-id": "<anonymous-schema-76>"
                  }
                },
                "x-parser-schema-id": "RackElectricalIsolationRequestFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "RackElectricalIsolationRequestMetadata"
          },
          "x-parser-unique-object-id": "rackElectricalIsolationRequest",
          "x-parser-message-name": "RackElectricalIsolationRequestMsg"
        }
      },
      "x-parser-unique-object-id": "rackMetadata"
    },
    "rackIntegrationValue": {
      "address": "BMS/v1/{integration}/Value/Rack/{pointType}/{tagPath}",
      "description": "Values published by integrations for Rack control points.\n\n**MQTT wildcard examples**\n- All integration rack values: `BMS/v1/+/Value/Rack/#`\n",
      "parameters": {
        "integration": {
          "description": "Integration identifier."
        },
        "pointType": {
          "enum": [
            "RackLiquidIsolationRequest",
            "RackElectricalIsolationRequest"
          ],
          "description": "Integration-published Rack point type."
        },
        "tagPath": {
          "description": "Must match the tagPath from the corresponding BMS metadata exactly."
        }
      },
      "messages": {
        "valueMessage": "$ref:$.channels.rackBmsValue.messages.valueMessage"
      },
      "x-parser-unique-object-id": "rackIntegrationValue"
    },
    "powerMeterValue": {
      "address": "BMS/v1/PUB/Value/PowerMeter/{pointType}/{tagPath}",
      "description": "Real-time values published by the BMS for all PowerMeter points.\n\n**MQTT wildcard examples**\n- All power meter values: `BMS/v1/PUB/Value/PowerMeter/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "Voltage",
            "PowerFactor",
            "Frequency",
            "ApparentPower",
            "ActivePower",
            "Current",
            "CurrentLimit",
            "PhaseCurrent",
            "GenericPoint"
          ],
          "description": "PowerMeter point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "valueMessage": "$ref:$.channels.rackBmsValue.messages.valueMessage"
      },
      "x-parser-unique-object-id": "powerMeterValue"
    },
    "powerMeterMetadata": {
      "address": "BMS/v1/PUB/Metadata/PowerMeter/{pointType}/{tagPath}",
      "description": "BMS-published metadata for all PowerMeter points.\n\n**MQTT wildcard examples**\n- All power meter metadata: `BMS/v1/PUB/Metadata/PowerMeter/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "Voltage",
            "PowerFactor",
            "Frequency",
            "ApparentPower",
            "ActivePower",
            "Current",
            "CurrentLimit",
            "PhaseCurrent",
            "GenericPoint"
          ],
          "description": "PowerMeter point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "voltage": {
          "payload": {
            "allOf": [
              {
                "description": "Composed base for all PowerMeter metadata messages. Combines MetadataBase with PowerMeterIdentifiers.\n",
                "allOf": [
                  "$ref:$.channels.rackMetadata.messages.rackLiquidSupplyTemperature.payload.allOf[0].allOf[0]",
                  {
                    "type": "object",
                    "description": "PowerMeter-specific identifier fields added to all PowerMeter metadata messages.",
                    "required": [
                      "objectName",
                      "objectId",
                      "servesId"
                    ],
                    "properties": {
                      "objectName": {
                        "type": "string",
                        "description": "Human-readable name of the electrical device.",
                        "x-parser-schema-id": "<anonymous-schema-87>"
                      },
                      "objectId": {
                        "type": "string",
                        "description": "Stable unique identifier for the electrical device.",
                        "x-parser-schema-id": "<anonymous-schema-88>"
                      },
                      "servesId": {
                        "type": "array",
                        "items": {
                          "type": "string",
                          "x-parser-schema-id": "<anonymous-schema-90>"
                        },
                        "description": "List of objectIds of entities served by this power meter.",
                        "x-parser-schema-id": "<anonymous-schema-89>"
                      }
                    },
                    "x-parser-schema-id": "PowerMeterIdentifiers"
                  }
                ],
                "x-parser-schema-id": "PowerMeterMetadataBase"
              },
              {
                "type": "object",
                "required": [
                  "engUnit"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "Voltage"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-91>"
                  },
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "PowerMeter"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-92>"
                  },
                  "engUnit": {
                    "type": "string",
                    "x-parser-schema-id": "<anonymous-schema-93>"
                  }
                },
                "x-parser-schema-id": "VoltageFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "PowerMeterVoltageMetadata"
          },
          "x-parser-unique-object-id": "voltage",
          "x-parser-message-name": "PowerMeterVoltageMsg"
        },
        "powerFactor": {
          "payload": {
            "allOf": [
              "$ref:$.channels.powerMeterMetadata.messages.voltage.payload.allOf[0]",
              {
                "type": "object",
                "description": "Dimensionless 0–1. engUnit not required.",
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "PowerFactor"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-94>"
                  },
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "PowerMeter"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-95>"
                  }
                },
                "x-parser-schema-id": "PowerFactorFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "PowerMeterPowerFactorMetadata"
          },
          "x-parser-unique-object-id": "powerFactor",
          "x-parser-message-name": "PowerMeterPowerFactorMsg"
        },
        "frequency": {
          "payload": {
            "allOf": [
              "$ref:$.channels.powerMeterMetadata.messages.voltage.payload.allOf[0]",
              {
                "type": "object",
                "required": [
                  "engUnit"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "Frequency"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-96>"
                  },
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "PowerMeter"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-97>"
                  },
                  "engUnit": {
                    "type": "string",
                    "x-parser-schema-id": "<anonymous-schema-98>"
                  }
                },
                "x-parser-schema-id": "FrequencyFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "PowerMeterFrequencyMetadata"
          },
          "x-parser-unique-object-id": "frequency",
          "x-parser-message-name": "PowerMeterFrequencyMsg"
        },
        "apparentPower": {
          "payload": {
            "allOf": [
              "$ref:$.channels.powerMeterMetadata.messages.voltage.payload.allOf[0]",
              {
                "type": "object",
                "required": [
                  "engUnit"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "ApparentPower"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-99>"
                  },
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "PowerMeter"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-100>"
                  },
                  "engUnit": {
                    "type": "string",
                    "x-parser-schema-id": "<anonymous-schema-101>"
                  }
                },
                "x-parser-schema-id": "ApparentPowerFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "PowerMeterApparentPowerMetadata"
          },
          "x-parser-unique-object-id": "apparentPower",
          "x-parser-message-name": "PowerMeterApparentPowerMsg"
        },
        "activePower": {
          "payload": {
            "allOf": [
              "$ref:$.channels.powerMeterMetadata.messages.voltage.payload.allOf[0]",
              {
                "type": "object",
                "required": [
                  "engUnit"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "ActivePower"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-102>"
                  },
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "PowerMeter"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-103>"
                  },
                  "engUnit": {
                    "type": "string",
                    "x-parser-schema-id": "<anonymous-schema-104>"
                  }
                },
                "x-parser-schema-id": "ActivePowerFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "PowerMeterActivePowerMetadata"
          },
          "x-parser-unique-object-id": "activePower",
          "x-parser-message-name": "PowerMeterActivePowerMsg"
        },
        "current": {
          "payload": {
            "allOf": [
              "$ref:$.channels.powerMeterMetadata.messages.voltage.payload.allOf[0]",
              {
                "type": "object",
                "required": [
                  "engUnit"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "Current"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-105>"
                  },
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "PowerMeter"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-106>"
                  },
                  "engUnit": {
                    "type": "string",
                    "x-parser-schema-id": "<anonymous-schema-107>"
                  }
                },
                "x-parser-schema-id": "CurrentFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "PowerMeterCurrentMetadata"
          },
          "x-parser-unique-object-id": "current",
          "x-parser-message-name": "PowerMeterCurrentMsg"
        },
        "currentLimit": {
          "payload": {
            "allOf": [
              "$ref:$.channels.powerMeterMetadata.messages.voltage.payload.allOf[0]",
              {
                "type": "object",
                "required": [
                  "engUnit"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "CurrentLimit"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-108>"
                  },
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "PowerMeter"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-109>"
                  },
                  "engUnit": {
                    "type": "string",
                    "x-parser-schema-id": "<anonymous-schema-110>"
                  }
                },
                "x-parser-schema-id": "CurrentLimitFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "PowerMeterCurrentLimitMetadata"
          },
          "x-parser-unique-object-id": "currentLimit",
          "x-parser-message-name": "PowerMeterCurrentLimitMsg"
        },
        "genericPoint": {
          "payload": {
            "description": "Metadata for a GenericPoint on PowerMeter equipment.\nInherits the standard PowerMeter identifiers\n(objectName, objectId, servesId all required).\n`processArea` is required. `engUnit`, `stateText`, `phase`, and\n`integration` are optional.\n",
            "allOf": [
              "$ref:$.channels.powerMeterMetadata.messages.voltage.payload.allOf[0]",
              {
                "type": "object",
                "description": "Field fragment for a vendor-specific or unmapped GenericPoint. `processArea` is required.\n\n`engUnit` and `stateText` are both optional but **mutually exclusive** — include at most one. See the two variants below.\n",
                "required": [
                  "processArea"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "GenericPoint"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-111>"
                  },
                  "processArea": {
                    "type": "array",
                    "items": {
                      "type": "string",
                      "x-parser-schema-id": "<anonymous-schema-113>"
                    },
                    "description": "Required for GenericPoint. Describes the measurement context.\n",
                    "x-parser-schema-id": "<anonymous-schema-112>"
                  },
                  "phase": {
                    "type": "string",
                    "enum": [
                      "A",
                      "B",
                      "C"
                    ],
                    "description": "Optional electrical phase identifier. Use for phase-specific generic measurements on power-capable equipment.\n",
                    "x-parser-schema-id": "<anonymous-schema-114>"
                  },
                  "integration": {
                    "type": "string",
                    "description": "Optional integration identifier. When present, this integration is responsible for publishing the value for this point. The value topic is derived using the standard topic derivation rule.\n",
                    "x-parser-schema-id": "<anonymous-schema-115>"
                  },
                  "isSetpoint": {
                    "type": "boolean",
                    "description": "Optional. When true, indicates this point is a setpoint (a target value written to control equipment behavior).\n",
                    "x-parser-schema-id": "<anonymous-schema-116>"
                  }
                },
                "anyOf": [
                  {
                    "title": "Measurement",
                    "description": "Continuous measurement with a known engineering unit. When `engUnit` is present, `stateText` must be absent.\n",
                    "properties": {
                      "engUnit": {
                        "type": "string",
                        "description": "Engineering unit for the measurement.\n"
                      }
                    },
                    "not": {
                      "required": [
                        "stateText"
                      ],
                      "x-parser-schema-id": "<anonymous-schema-118>"
                    },
                    "x-parser-schema-id": "<anonymous-schema-117>"
                  },
                  {
                    "title": "State",
                    "description": "Binary or enumerated state point. When `stateText` is present, `engUnit` must be absent.\n",
                    "properties": {
                      "stateText": {
                        "type": "array",
                        "description": "State label mapping. Each entry maps a numeric state value to its human-readable label (e.g., [{value: 0, text: \"Off\"}, {value: 1, text: \"On\"}]).\n",
                        "items": {
                          "type": "object",
                          "required": [
                            "value",
                            "text"
                          ],
                          "properties": {
                            "value": {
                              "type": "integer",
                              "description": "Numeric state value."
                            },
                            "text": {
                              "type": "string",
                              "description": "Human-readable label for this state."
                            }
                          },
                          "additionalProperties": false
                        }
                      }
                    },
                    "not": {
                      "required": [
                        "engUnit"
                      ],
                      "x-parser-schema-id": "<anonymous-schema-120>"
                    },
                    "x-parser-schema-id": "<anonymous-schema-119>"
                  }
                ],
                "x-parser-schema-id": "GenericPointFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "GenericPowerMeterPointMetadata"
          },
          "x-parser-unique-object-id": "genericPoint",
          "x-parser-message-name": "GenericPowerMeterPointMsg"
        },
        "phaseCurrent": {
          "payload": {
            "allOf": [
              "$ref:$.channels.powerMeterMetadata.messages.voltage.payload.allOf[0]",
              {
                "type": "object",
                "description": "PhaseCurrent additionally requires `phase` to identify which electrical phase (A, B, or C). 3 metadata messages per meter, one per phase.\n",
                "required": [
                  "engUnit",
                  "phase"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "PhaseCurrent"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-121>"
                  },
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "PowerMeter"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-122>"
                  },
                  "engUnit": {
                    "type": "string",
                    "x-parser-schema-id": "<anonymous-schema-123>"
                  },
                  "phase": {
                    "type": "string",
                    "enum": [
                      "A",
                      "B",
                      "C"
                    ],
                    "description": "Electrical phase identifier.",
                    "x-parser-schema-id": "<anonymous-schema-124>"
                  }
                },
                "x-parser-schema-id": "PhaseCurrentFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "PowerMeterPhaseCurrentMetadata"
          },
          "x-parser-unique-object-id": "phaseCurrent",
          "x-parser-message-name": "PowerMeterPhaseCurrentMsg"
        }
      },
      "x-parser-unique-object-id": "powerMeterMetadata"
    },
    "bessValue": {
      "address": "BMS/v1/PUB/Value/BESS/{pointType}/{tagPath}",
      "description": "Real-time values published by the BMS for BESS points.\nExtensible: additional vendor-specific pointTypes may be present.\n\n**MQTT wildcard examples**\n- All BESS values: `BMS/v1/PUB/Value/BESS/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "Status",
            "Available",
            "GenericPoint"
          ],
          "description": "BMS-published BESS point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "valueMessage": "$ref:$.channels.rackBmsValue.messages.valueMessage"
      },
      "x-parser-unique-object-id": "bessValue"
    },
    "bessMetadata": {
      "address": "BMS/v1/PUB/Metadata/BESS/{pointType}/{tagPath}",
      "description": "BMS-published metadata for BESS points.\n\n**MQTT wildcard examples**\n- All BESS metadata: `BMS/v1/PUB/Metadata/BESS/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "Status",
            "Available",
            "GenericPoint"
          ],
          "description": "BESS point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "status": {
          "payload": {
            "allOf": [
              {
                "description": "Composed base for all generic equipment metadata.\n\nCombines MetadataBase + EquipmentCommonFields + exactly one identifier\nmode from the `oneOf`:\n- **ObjectMode**: objectName + objectId required,\n  servesId optional, associateId prohibited.\n- **Associate Mode**: associateId required, all name/id/serves\n  fields prohibited.\n",
                "allOf": [
                  "$ref:$.channels.rackMetadata.messages.rackLiquidSupplyTemperature.payload.allOf[0].allOf[0]",
                  {
                    "type": "object",
                    "description": "Optional fields common to all generic equipment metadata, regardless of identifier mode.\n",
                    "properties": {
                      "processArea": {
                        "type": "array",
                        "items": {
                          "type": "string",
                          "x-parser-schema-id": "<anonymous-schema-130>"
                        },
                        "description": "List of process areas or sub-system locations within the equipment\n",
                        "x-parser-schema-id": "<anonymous-schema-129>"
                      }
                    },
                    "x-parser-schema-id": "EquipmentCommonFields"
                  },
                  {
                    "oneOf": [
                      {
                        "type": "object",
                        "description": "**Object Mode**: use when the object is identified directly\nby name and ID.\n\n- `objectName` and `objectId` are **required**.\n- `servesId` is **optional** in Named-object mode.\n- `associateId` must **not** be present.\n\nIncompatible with `EquipmentAssociateMode` — validators enforce this via\nthe parent `oneOf`.\n",
                        "required": [
                          "objectName",
                          "objectId"
                        ],
                        "properties": {
                          "objectName": {
                            "type": "string",
                            "description": "Human-readable equipment name.",
                            "x-parser-schema-id": "<anonymous-schema-132>"
                          },
                          "objectId": {
                            "type": "string",
                            "description": "Stable unique identifier for the equipment.",
                            "x-parser-schema-id": "<anonymous-schema-133>"
                          },
                          "servesId": {
                            "type": "array",
                            "items": {
                              "type": "string",
                              "x-parser-schema-id": "<anonymous-schema-135>"
                            },
                            "description": "Optional list of objectIds of entities this equipment serves. Only valid in Named-object mode. Only valid in Named-object mode — must not appear in Associate mode.\n",
                            "x-parser-schema-id": "<anonymous-schema-134>"
                          }
                        },
                        "not": {
                          "properties": {
                            "associateId": {
                              "type": "string"
                            }
                          },
                          "required": [
                            "associateId"
                          ],
                          "x-parser-schema-id": "<anonymous-schema-136>"
                        },
                        "x-parser-schema-id": "EquipmentNamedObjectMode"
                      },
                      {
                        "type": "object",
                        "description": "*Associate Mode*: use when the object is referenced via an\nassociation identifier.\n\n- `associateId` is **required**.\n- `objectName`, `objectId`, and `servesId` must **not** be present.\n\nIncompatible with `EquipmentNamedObjectMode` — validators enforce this\nvia the parent `oneOf`.\n",
                        "required": [
                          "associateId"
                        ],
                        "properties": {
                          "associateId": {
                            "type": "string",
                            "description": "Identifier of the associated entity.",
                            "x-parser-schema-id": "<anonymous-schema-137>"
                          }
                        },
                        "not": {
                          "anyOf": [
                            {
                              "properties": {
                                "objectName": {
                                  "type": "string"
                                }
                              },
                              "required": [
                                "objectName"
                              ],
                              "x-parser-schema-id": "<anonymous-schema-139>"
                            },
                            {
                              "properties": {
                                "objectId": {
                                  "type": "string"
                                }
                              },
                              "required": [
                                "objectId"
                              ],
                              "x-parser-schema-id": "<anonymous-schema-140>"
                            },
                            {
                              "properties": {
                                "servesId": {
                                  "type": "array",
                                  "items": {
                                    "type": "string"
                                  }
                                }
                              },
                              "required": [
                                "servesId"
                              ],
                              "x-parser-schema-id": "<anonymous-schema-141>"
                            }
                          ],
                          "x-parser-schema-id": "<anonymous-schema-138>"
                        },
                        "x-parser-schema-id": "EquipmentAssociateMode"
                      }
                    ],
                    "x-parser-schema-id": "<anonymous-schema-131>"
                  }
                ],
                "x-parser-schema-id": "EquipmentMetadataBase"
              },
              {
                "type": "object",
                "required": [
                  "stateText"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "Status"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-142>"
                  },
                  "integration": {
                    "type": "string",
                    "description": "Optional integration identifier. When present, this integration is responsible for publishing the value for this point.\n",
                    "x-parser-schema-id": "<anonymous-schema-143>"
                  },
                  "stateText": {
                    "type": "array",
                    "description": "State label mapping for operating status (values vary by equipment vendor).\n",
                    "items": {
                      "type": "object",
                      "required": [
                        "value",
                        "text"
                      ],
                      "properties": {
                        "value": {
                          "type": "integer",
                          "description": "Numeric state value.",
                          "x-parser-schema-id": "<anonymous-schema-146>"
                        },
                        "text": {
                          "type": "string",
                          "description": "Human-readable label for this state.",
                          "x-parser-schema-id": "<anonymous-schema-147>"
                        }
                      },
                      "additionalProperties": false,
                      "x-parser-schema-id": "<anonymous-schema-145>"
                    },
                    "examples": [
                      [
                        {
                          "value": 0,
                          "text": "NotOperating"
                        },
                        {
                          "value": 1,
                          "text": "Operating"
                        }
                      ]
                    ],
                    "x-parser-schema-id": "<anonymous-schema-144>"
                  }
                },
                "x-parser-schema-id": "StatusFields"
              },
              {
                "type": "object",
                "properties": {
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "BESS"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-148>"
                  }
                },
                "x-parser-schema-id": "BESSObjectTypeFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "BESSStatusMetadata"
          },
          "x-parser-unique-object-id": "status",
          "x-parser-message-name": "BESSStatusMsg"
        },
        "genericPoint": {
          "payload": {
            "description": "Metadata for a GenericPoint on any generic equipment objectType.\n\nFollows the standard equipment identifier modes:\n- **Named-object mode**: objectName + objectId required; servesId\n  optional; associateId prohibited.\n- **Associate mode**: associateId required; objectName/objectId/servesId\n  prohibited.\n\nUnlike typed points, `engUnit` and `stateText` are both optional.\n`processArea` is required.\n",
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.powerMeterMetadata.messages.genericPoint.payload.allOf[1]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "GenericEquipmentPointMetadata"
          },
          "x-parser-unique-object-id": "genericPoint",
          "x-parser-message-name": "GenericEquipmentPointMsg"
        },
        "available": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              {
                "type": "object",
                "required": [
                  "stateText"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "Available"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-149>"
                  },
                  "integration": {
                    "type": "string",
                    "description": "Optional integration identifier. When present, this integration is responsible for publishing the value for this point.\n",
                    "x-parser-schema-id": "<anonymous-schema-150>"
                  },
                  "stateText": {
                    "type": "array",
                    "description": "State label mapping for availability status (values vary by equipment vendor).\n",
                    "items": {
                      "type": "object",
                      "required": [
                        "value",
                        "text"
                      ],
                      "properties": {
                        "value": {
                          "type": "integer",
                          "description": "Numeric state value.",
                          "x-parser-schema-id": "<anonymous-schema-153>"
                        },
                        "text": {
                          "type": "string",
                          "description": "Human-readable label for this state.",
                          "x-parser-schema-id": "<anonymous-schema-154>"
                        }
                      },
                      "additionalProperties": false,
                      "x-parser-schema-id": "<anonymous-schema-152>"
                    },
                    "examples": [
                      [
                        {
                          "value": 0,
                          "text": "NotAvailable"
                        },
                        {
                          "value": 1,
                          "text": "Available"
                        }
                      ]
                    ],
                    "x-parser-schema-id": "<anonymous-schema-151>"
                  }
                },
                "x-parser-schema-id": "AvailableFields"
              },
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "BESSAvailableMetadata"
          },
          "x-parser-unique-object-id": "available",
          "x-parser-message-name": "BESSAvailableMsg"
        }
      },
      "x-parser-unique-object-id": "bessMetadata"
    },
    "upsValue": {
      "address": "BMS/v1/PUB/Value/UPS/{pointType}/{tagPath}",
      "description": "Real-time values published by the BMS for UPS points.\nExtensible: additional vendor-specific pointTypes may be present.\n\n**MQTT wildcard examples**\n- All UPS values: `BMS/v1/PUB/Value/UPS/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "Status",
            "Available",
            "GenericPoint"
          ],
          "description": "BMS-published UPS point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "valueMessage": "$ref:$.channels.rackBmsValue.messages.valueMessage"
      },
      "x-parser-unique-object-id": "upsValue"
    },
    "upsMetadata": {
      "address": "BMS/v1/PUB/Metadata/UPS/{pointType}/{tagPath}",
      "description": "BMS-published metadata for UPS points.\n\n**MQTT wildcard examples**\n- All UPS metadata: `BMS/v1/PUB/Metadata/UPS/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "Status",
            "Available",
            "GenericPoint"
          ],
          "description": "UPS point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "status": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[1]",
              {
                "type": "object",
                "properties": {
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "UPS"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-159>"
                  }
                },
                "x-parser-schema-id": "UPSObjectTypeFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "UPSStatusMetadata"
          },
          "x-parser-unique-object-id": "status",
          "x-parser-message-name": "UPSStatusMsg"
        },
        "genericPoint": "$ref:$.channels.bessMetadata.messages.genericPoint",
        "available": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.available.payload.allOf[1]",
              "$ref:$.channels.upsMetadata.messages.status.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "UPSAvailableMetadata"
          },
          "x-parser-unique-object-id": "available",
          "x-parser-message-name": "UPSAvailableMsg"
        }
      },
      "x-parser-unique-object-id": "upsMetadata"
    },
    "atsValue": {
      "address": "BMS/v1/PUB/Value/ATS/{pointType}/{tagPath}",
      "description": "Real-time values published by the BMS for ATS points.\nExtensible: additional vendor-specific pointTypes may be present.\n\n**MQTT wildcard examples**\n- All ATS values: `BMS/v1/PUB/Value/ATS/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "Status",
            "Available",
            "GenericPoint"
          ],
          "description": "BMS-published ATS point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "valueMessage": "$ref:$.channels.rackBmsValue.messages.valueMessage"
      },
      "x-parser-unique-object-id": "atsValue"
    },
    "atsMetadata": {
      "address": "BMS/v1/PUB/Metadata/ATS/{pointType}/{tagPath}",
      "description": "BMS-published metadata for ATS points.\n\n**MQTT wildcard examples**\n- All ATS metadata: `BMS/v1/PUB/Metadata/ATS/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "Status",
            "Available",
            "GenericPoint"
          ],
          "description": "ATS point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "status": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[1]",
              {
                "type": "object",
                "properties": {
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "ATS"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-164>"
                  }
                },
                "x-parser-schema-id": "ATSObjectTypeFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "ATSStatusMetadata"
          },
          "x-parser-unique-object-id": "status",
          "x-parser-message-name": "ATSStatusMsg"
        },
        "genericPoint": "$ref:$.channels.bessMetadata.messages.genericPoint",
        "available": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.available.payload.allOf[1]",
              "$ref:$.channels.atsMetadata.messages.status.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "ATSAvailableMetadata"
          },
          "x-parser-unique-object-id": "available",
          "x-parser-message-name": "ATSAvailableMsg"
        }
      },
      "x-parser-unique-object-id": "atsMetadata"
    },
    "generatorValue": {
      "address": "BMS/v1/PUB/Value/Generator/{pointType}/{tagPath}",
      "description": "Real-time values published by the BMS for Generator points.\nExtensible: additional vendor-specific pointTypes may be present.\n\n**MQTT wildcard examples**\n- All Generator values: `BMS/v1/PUB/Value/Generator/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "Status",
            "Available",
            "GenericPoint"
          ],
          "description": "BMS-published Generator point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "valueMessage": "$ref:$.channels.rackBmsValue.messages.valueMessage"
      },
      "x-parser-unique-object-id": "generatorValue"
    },
    "generatorMetadata": {
      "address": "BMS/v1/PUB/Metadata/Generator/{pointType}/{tagPath}",
      "description": "BMS-published metadata for Generator points.\n\n**MQTT wildcard examples**\n- All Generator metadata: `BMS/v1/PUB/Metadata/Generator/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "Status",
            "Available",
            "GenericPoint"
          ],
          "description": "Generator point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "status": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[1]",
              {
                "type": "object",
                "properties": {
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "Generator"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-169>"
                  }
                },
                "x-parser-schema-id": "GeneratorObjectTypeFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "GeneratorStatusMetadata"
          },
          "x-parser-unique-object-id": "status",
          "x-parser-message-name": "GeneratorStatusMsg"
        },
        "genericPoint": "$ref:$.channels.bessMetadata.messages.genericPoint",
        "available": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.available.payload.allOf[1]",
              "$ref:$.channels.generatorMetadata.messages.status.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "GeneratorAvailableMetadata"
          },
          "x-parser-unique-object-id": "available",
          "x-parser-message-name": "GeneratorAvailableMsg"
        }
      },
      "x-parser-unique-object-id": "generatorMetadata"
    },
    "shuntValue": {
      "address": "BMS/v1/PUB/Value/Shunt/{pointType}/{tagPath}",
      "description": "Real-time values published by the BMS for Shunt points.\n\n**MQTT wildcard examples**\n- All Shunt values: `BMS/v1/PUB/Value/Shunt/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "Status",
            "Available",
            "GenericPoint"
          ],
          "description": "BMS-published Shunt point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "valueMessage": "$ref:$.channels.rackBmsValue.messages.valueMessage"
      },
      "x-parser-unique-object-id": "shuntValue"
    },
    "shuntMetadata": {
      "address": "BMS/v1/PUB/Metadata/Shunt/{pointType}/{tagPath}",
      "description": "BMS-published metadata for Shunt points.\n\n**MQTT wildcard examples**\n- All Shunt metadata: `BMS/v1/PUB/Metadata/Shunt/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "Status",
            "Available",
            "GenericPoint"
          ],
          "description": "Shunt point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "status": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[1]",
              {
                "type": "object",
                "properties": {
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "Shunt"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-174>"
                  }
                },
                "x-parser-schema-id": "ShuntObjectTypeFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "ShuntStatusMetadata"
          },
          "x-parser-unique-object-id": "status",
          "x-parser-message-name": "ShuntStatusMsg"
        },
        "genericPoint": "$ref:$.channels.bessMetadata.messages.genericPoint",
        "available": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.available.payload.allOf[1]",
              "$ref:$.channels.shuntMetadata.messages.status.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "ShuntAvailableMetadata"
          },
          "x-parser-unique-object-id": "available",
          "x-parser-message-name": "ShuntAvailableMsg"
        }
      },
      "x-parser-unique-object-id": "shuntMetadata"
    },
    "breakerValue": {
      "address": "BMS/v1/PUB/Value/Breaker/{pointType}/{tagPath}",
      "description": "Real-time values published by the BMS for Breaker points.\n\n**MQTT wildcard examples**\n- All Breaker values: `BMS/v1/PUB/Value/Breaker/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "Status",
            "Available",
            "GenericPoint"
          ],
          "description": "BMS-published Breaker point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "valueMessage": "$ref:$.channels.rackBmsValue.messages.valueMessage"
      },
      "x-parser-unique-object-id": "breakerValue"
    },
    "breakerMetadata": {
      "address": "BMS/v1/PUB/Metadata/Breaker/{pointType}/{tagPath}",
      "description": "BMS-published metadata for Breaker points.\n\n**MQTT wildcard examples**\n- All Breaker metadata: `BMS/v1/PUB/Metadata/Breaker/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "Status",
            "Available",
            "GenericPoint"
          ],
          "description": "Breaker point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "status": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[1]",
              {
                "type": "object",
                "properties": {
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "Breaker"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-179>"
                  }
                },
                "x-parser-schema-id": "BreakerObjectTypeFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "BreakerStatusMetadata"
          },
          "x-parser-unique-object-id": "status",
          "x-parser-message-name": "BreakerStatusMsg"
        },
        "genericPoint": "$ref:$.channels.bessMetadata.messages.genericPoint",
        "available": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.available.payload.allOf[1]",
              "$ref:$.channels.breakerMetadata.messages.status.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "BreakerAvailableMetadata"
          },
          "x-parser-unique-object-id": "available",
          "x-parser-message-name": "BreakerAvailableMsg"
        }
      },
      "x-parser-unique-object-id": "breakerMetadata"
    },
    "cduValue": {
      "address": "BMS/v1/PUB/Value/CDU/{pointType}/{tagPath}",
      "description": "Real-time values published by the BMS for CDU points.\n\n**MQTT wildcard examples**\n- All CDU values: `BMS/v1/PUB/Value/CDU/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "LiquidTemperature",
            "LiquidDifferentialPressure",
            "LiquidFlow",
            "LiquidPressure",
            "Status",
            "Available",
            "ValvePosition",
            "PumpSpeed",
            "FanSpeed",
            "DamperPosition",
            "AirTemperature",
            "AirDifferentialPressure",
            "AirRelativeHumidity",
            "AirFlow",
            "AirPressure",
            "LeakDetect",
            "GenericPoint"
          ],
          "description": "BMS-published CDU point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "valueMessage": "$ref:$.channels.rackBmsValue.messages.valueMessage"
      },
      "x-parser-unique-object-id": "cduValue"
    },
    "cduMetadata": {
      "address": "BMS/v1/PUB/Metadata/CDU/{pointType}/{tagPath}",
      "description": "BMS-published metadata for CDU points.\nIncludes integration-owned points which carries the `integration` field.\n\n**MQTT wildcard examples**\n- All CDU metadata: `BMS/v1/PUB/Metadata/CDU/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "LiquidTemperature",
            "LiquidDifferentialPressure",
            "LiquidFlow",
            "LiquidPressure",
            "Status",
            "Available",
            "ValvePosition",
            "PumpSpeed",
            "FanSpeed",
            "DamperPosition",
            "AirTemperature",
            "AirDifferentialPressure",
            "AirRelativeHumidity",
            "AirFlow",
            "AirPressure",
            "LeakDetect",
            "LiquidTemperatureSpRequest",
            "GenericPoint"
          ],
          "description": "CDU point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "liquidTemperature": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              {
                "type": "object",
                "required": [
                  "engUnit"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "LiquidTemperature"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-184>"
                  },
                  "engUnit": {
                    "type": "string",
                    "enum": [
                      "C"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-185>"
                  },
                  "isSetpoint": {
                    "type": "boolean",
                    "description": "Optional. When true, indicates this point is a setpoint (a target value written to control equipment behavior).\n",
                    "x-parser-schema-id": "<anonymous-schema-186>"
                  }
                },
                "x-parser-schema-id": "LiquidTemperatureFields"
              },
              {
                "type": "object",
                "properties": {
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "CDU"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-187>"
                  }
                },
                "x-parser-schema-id": "CDUObjectTypeFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CDULiquidTemperatureMetadata"
          },
          "x-parser-unique-object-id": "liquidTemperature",
          "x-parser-message-name": "CDULiquidTemperatureMsg"
        },
        "liquidDifferentialPressure": {
          "payload": {
            "allOf": [
              {
                "description": "Base for equipment measurement metadata. Two independent XOR constraints apply: - Identifier: named-object mode (objectName + objectId required,\n  associateId prohibited) XOR associate mode (associateId required, no objectName/objectId).\n- Measurement: engUnit required XOR stateText required. These two constraints are fully independent — all four combinations are valid. Combines MetadataBase, EquipmentCommonFields, and both independent oneOf constraints.\n",
                "allOf": [
                  "$ref:$.channels.rackMetadata.messages.rackLiquidSupplyTemperature.payload.allOf[0].allOf[0]",
                  "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0].allOf[1]",
                  {
                    "oneOf": [
                      "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0].allOf[2].oneOf[0]",
                      "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0].allOf[2].oneOf[1]"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-188>"
                  },
                  {
                    "oneOf": [
                      {
                        "type": "object",
                        "description": "Requires a non-empty engUnit string (mutually exclusive with stateText).",
                        "required": [
                          "engUnit"
                        ],
                        "properties": {
                          "engUnit": {
                            "type": "string",
                            "description": "Engineering unit for the measurement.",
                            "x-parser-schema-id": "<anonymous-schema-190>"
                          }
                        },
                        "x-parser-schema-id": "EquipmentPointEngUnit"
                      },
                      {
                        "type": "object",
                        "description": "Required for state/status/alarm points that carry no engineering unit.",
                        "required": [
                          "stateText"
                        ],
                        "properties": {
                          "stateText": {
                            "type": "array",
                            "description": "State label mapping. Each entry maps a numeric state value to its human-readable label (e.g., [{value: 0, text: \"Off\"}, {value: 1, text: \"On\"}]).\n",
                            "items": {
                              "type": "object",
                              "required": [
                                "value",
                                "text"
                              ],
                              "properties": {
                                "value": {
                                  "type": "integer",
                                  "description": "Numeric state value.",
                                  "x-parser-schema-id": "<anonymous-schema-193>"
                                },
                                "text": {
                                  "type": "string",
                                  "description": "Human-readable label for this state.",
                                  "x-parser-schema-id": "<anonymous-schema-194>"
                                }
                              },
                              "additionalProperties": false,
                              "x-parser-schema-id": "<anonymous-schema-192>"
                            },
                            "x-parser-schema-id": "<anonymous-schema-191>"
                          }
                        },
                        "x-parser-schema-id": "StateTextField"
                      }
                    ],
                    "x-parser-schema-id": "<anonymous-schema-189>"
                  }
                ],
                "x-parser-schema-id": "EquipmentMeasurementModeBase"
              },
              {
                "type": "object",
                "description": "Measurement fields for LiquidDifferentialPressure. Typical engUnit: kPa. The identifier (named-object XOR associate) and measurement (engUnit XOR stateText) constraints are independent and enforced by EquipmentMeasurementModeBase.\n",
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "LiquidDifferentialPressure"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-195>"
                  },
                  "isSetpoint": {
                    "type": "boolean",
                    "description": "Optional. When true, indicates this point is a setpoint (a target value written to control equipment behavior).\n",
                    "x-parser-schema-id": "<anonymous-schema-196>"
                  }
                },
                "x-parser-schema-id": "LiquidDifferentialPressureFields"
              },
              "$ref:$.channels.cduMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CDULiquidDifferentialPressureMetadata"
          },
          "x-parser-unique-object-id": "liquidDifferentialPressure",
          "x-parser-message-name": "CDULiquidDifferentialPressureMsg"
        },
        "liquidFlow": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              {
                "type": "object",
                "description": "Measurement fields for LiquidFlow. Typical engUnit: LPM. The identifier (named-object XOR associate) and measurement (engUnit XOR stateText) constraints are independent and enforced by EquipmentMeasurementModeBase.\n",
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "LiquidFlow"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-197>"
                  },
                  "isSetpoint": {
                    "type": "boolean",
                    "description": "Optional. When true, indicates this point is a setpoint (a target value written to control equipment behavior).\n",
                    "x-parser-schema-id": "<anonymous-schema-198>"
                  }
                },
                "x-parser-schema-id": "LiquidFlowFields"
              },
              "$ref:$.channels.cduMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CDULiquidFlowMetadata"
          },
          "x-parser-unique-object-id": "liquidFlow",
          "x-parser-message-name": "CDULiquidFlowMsg"
        },
        "liquidPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              {
                "type": "object",
                "description": "Measurement fields for LiquidPressure. Typical engUnit: kPa. The identifier (named-object XOR associate) and measurement (engUnit XOR stateText) constraints are independent and enforced by EquipmentMeasurementModeBase.\n",
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "LiquidPressure"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-199>"
                  },
                  "isSetpoint": {
                    "type": "boolean",
                    "description": "Optional. When true, indicates this point is a setpoint (a target value written to control equipment behavior).\n",
                    "x-parser-schema-id": "<anonymous-schema-200>"
                  }
                },
                "x-parser-schema-id": "LiquidPressureFields"
              },
              "$ref:$.channels.cduMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CDULiquidPressureMetadata"
          },
          "x-parser-unique-object-id": "liquidPressure",
          "x-parser-message-name": "CDULiquidPressureMsg"
        },
        "status": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[1]",
              "$ref:$.channels.cduMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CDUStatusMetadata"
          },
          "x-parser-unique-object-id": "status",
          "x-parser-message-name": "CDUStatusMsg"
        },
        "available": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.available.payload.allOf[1]",
              "$ref:$.channels.cduMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CDUAvailableMetadata"
          },
          "x-parser-unique-object-id": "available",
          "x-parser-message-name": "CDUAvailableMsg"
        },
        "valvePosition": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              {
                "type": "object",
                "description": "Measurement fields for ValvePosition. Typical engUnit: %. The identifier (named-object XOR associate) and measurement (engUnit XOR stateText) constraints are independent and enforced by EquipmentMeasurementModeBase.\n",
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "ValvePosition"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-201>"
                  }
                },
                "x-parser-schema-id": "ValvePositionFields"
              },
              "$ref:$.channels.cduMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CDUValvePositionMetadata"
          },
          "x-parser-unique-object-id": "valvePosition",
          "x-parser-message-name": "CDUValvePositionMsg"
        },
        "pumpSpeed": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              {
                "type": "object",
                "description": "Measurement fields for PumpSpeed. Typical engUnit: RPM. The identifier (named-object XOR associate) and measurement (engUnit XOR stateText) constraints are independent and enforced by EquipmentMeasurementModeBase.\n",
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "PumpSpeed"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-202>"
                  }
                },
                "x-parser-schema-id": "PumpSpeedFields"
              },
              "$ref:$.channels.cduMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CDUPumpSpeedMetadata"
          },
          "x-parser-unique-object-id": "pumpSpeed",
          "x-parser-message-name": "CDUPumpSpeedMsg"
        },
        "fanSpeed": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              {
                "type": "object",
                "description": "Measurement fields for FanSpeed. Typical engUnit: RPM. The identifier (named-object XOR associate) and measurement (engUnit XOR stateText) constraints are independent and enforced by EquipmentMeasurementModeBase.\n",
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "FanSpeed"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-203>"
                  }
                },
                "x-parser-schema-id": "FanSpeedFields"
              },
              "$ref:$.channels.cduMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CDUFanSpeedMetadata"
          },
          "x-parser-unique-object-id": "fanSpeed",
          "x-parser-message-name": "CDUFanSpeedMsg"
        },
        "damperPosition": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              {
                "type": "object",
                "description": "Measurement fields for DamperPosition. Typical engUnit: %. The identifier (named-object XOR associate) and measurement (engUnit XOR stateText) constraints are independent and enforced by EquipmentMeasurementModeBase.\n",
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "DamperPosition"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-204>"
                  }
                },
                "x-parser-schema-id": "DamperPositionFields"
              },
              "$ref:$.channels.cduMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CDUDamperPositionMetadata"
          },
          "x-parser-unique-object-id": "damperPosition",
          "x-parser-message-name": "CDUDamperPositionMsg"
        },
        "airTemperature": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              {
                "type": "object",
                "required": [
                  "engUnit"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "AirTemperature"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-205>"
                  },
                  "engUnit": {
                    "type": "string",
                    "enum": [
                      "C"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-206>"
                  },
                  "isSetpoint": {
                    "type": "boolean",
                    "description": "Optional. When true, indicates this point is a setpoint (a target value written to control equipment behavior).\n",
                    "x-parser-schema-id": "<anonymous-schema-207>"
                  }
                },
                "x-parser-schema-id": "AirTemperatureFields"
              },
              "$ref:$.channels.cduMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CDUAirTemperatureMetadata"
          },
          "x-parser-unique-object-id": "airTemperature",
          "x-parser-message-name": "CDUAirTemperatureMsg"
        },
        "airDifferentialPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              {
                "type": "object",
                "description": "Measurement fields for AirDifferentialPressure. Typical engUnit: Pa. The identifier (named-object XOR associate) and measurement (engUnit XOR stateText) constraints are independent and enforced by EquipmentMeasurementModeBase.\n",
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "AirDifferentialPressure"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-208>"
                  },
                  "isSetpoint": {
                    "type": "boolean",
                    "description": "Optional. When true, indicates this point is a setpoint (a target value written to control equipment behavior).\n",
                    "x-parser-schema-id": "<anonymous-schema-209>"
                  }
                },
                "x-parser-schema-id": "AirDifferentialPressureFields"
              },
              "$ref:$.channels.cduMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CDUAirDifferentialPressureMetadata"
          },
          "x-parser-unique-object-id": "airDifferentialPressure",
          "x-parser-message-name": "CDUAirDifferentialPressureMsg"
        },
        "airRelativeHumidity": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              {
                "type": "object",
                "description": "Measurement fields for AirRelativeHumidity. Typical engUnit: %RH. The identifier (named-object XOR associate) and measurement (engUnit XOR stateText) constraints are independent and enforced by EquipmentMeasurementModeBase.\n",
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "AirRelativeHumidity"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-210>"
                  },
                  "isSetpoint": {
                    "type": "boolean",
                    "description": "Optional. When true, indicates this point is a setpoint (a target value written to control equipment behavior).\n",
                    "x-parser-schema-id": "<anonymous-schema-211>"
                  }
                },
                "x-parser-schema-id": "AirRelativeHumidityFields"
              },
              "$ref:$.channels.cduMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CDUAirRelativeHumidityMetadata"
          },
          "x-parser-unique-object-id": "airRelativeHumidity",
          "x-parser-message-name": "CDUAirRelativeHumidityMsg"
        },
        "airFlow": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              {
                "type": "object",
                "description": "Measurement fields for AirFlow. Typical engUnit: CFM. The identifier (named-object XOR associate) and measurement (engUnit XOR stateText) constraints are independent and enforced by EquipmentMeasurementModeBase.\n",
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "AirFlow"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-212>"
                  },
                  "isSetpoint": {
                    "type": "boolean",
                    "description": "Optional. When true, indicates this point is a setpoint (a target value written to control equipment behavior).\n",
                    "x-parser-schema-id": "<anonymous-schema-213>"
                  }
                },
                "x-parser-schema-id": "AirFlowFields"
              },
              "$ref:$.channels.cduMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CDUAirFlowMetadata"
          },
          "x-parser-unique-object-id": "airFlow",
          "x-parser-message-name": "CDUAirFlowMsg"
        },
        "airPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              {
                "type": "object",
                "description": "Measurement fields for AirPressure. Typical engUnit: Pa. The identifier (named-object XOR associate) and measurement (engUnit XOR stateText) constraints are independent and enforced by EquipmentMeasurementModeBase.\n",
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "AirPressure"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-214>"
                  },
                  "isSetpoint": {
                    "type": "boolean",
                    "description": "Optional. When true, indicates this point is a setpoint (a target value written to control equipment behavior).\n",
                    "x-parser-schema-id": "<anonymous-schema-215>"
                  }
                },
                "x-parser-schema-id": "AirPressureFields"
              },
              "$ref:$.channels.cduMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CDUAirPressureMetadata"
          },
          "x-parser-unique-object-id": "airPressure",
          "x-parser-message-name": "CDUAirPressureMsg"
        },
        "leakDetect": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              {
                "type": "object",
                "description": "0 = No Leak, 1 = Leak. No engUnit.",
                "required": [
                  "stateText"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "LeakDetect"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-216>"
                  },
                  "stateText": {
                    "type": "array",
                    "description": "State label mapping for leak detection.\n",
                    "items": {
                      "type": "object",
                      "required": [
                        "value",
                        "text"
                      ],
                      "properties": {
                        "value": {
                          "type": "integer",
                          "description": "Numeric state value.",
                          "x-parser-schema-id": "<anonymous-schema-219>"
                        },
                        "text": {
                          "type": "string",
                          "description": "Human-readable label for this state.",
                          "x-parser-schema-id": "<anonymous-schema-220>"
                        }
                      },
                      "additionalProperties": false,
                      "x-parser-schema-id": "<anonymous-schema-218>"
                    },
                    "examples": [
                      [
                        {
                          "value": 0,
                          "text": "NoLeak"
                        },
                        {
                          "value": 1,
                          "text": "Leak"
                        }
                      ]
                    ],
                    "x-parser-schema-id": "<anonymous-schema-217>"
                  }
                },
                "x-parser-schema-id": "LeakDetectFields"
              },
              "$ref:$.channels.cduMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CDULeakDetectMetadata"
          },
          "x-parser-unique-object-id": "leakDetect",
          "x-parser-message-name": "CDULeakDetectMsg"
        },
        "liquidTemperatureSpRequest": {
          "payload": {
            "allOf": [
              {
                "description": "Extends EquipmentMetadataBase for integration-published equipment points. Adds EquipmentIntegrationIdentifierFields (integration required).\n",
                "allOf": [
                  "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
                  {
                    "type": "object",
                    "description": "Extends EquipmentIntegrationMetadataBase: adds `integration` for integration-published equipment points.\n",
                    "required": [
                      "integration"
                    ],
                    "properties": {
                      "integration": {
                        "type": "string",
                        "description": "Integration responsible for publishing this value.",
                        "x-parser-schema-id": "<anonymous-schema-221>"
                      }
                    },
                    "x-parser-schema-id": "EquipmentIntegrationIdentifierFields"
                  }
                ],
                "x-parser-schema-id": "EquipmentIntegrationMetadataBase"
              },
              {
                "type": "object",
                "description": "Setpoint request written by an integration (e.g., MEPAI) to a CDU. BMS publishes metadata; the integration publishes the value to respective value topic namespace identified by `integration`\n",
                "required": [
                  "engUnit"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "LiquidTemperatureSpRequest"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-222>"
                  },
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "CDU"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-223>"
                  },
                  "engUnit": {
                    "type": "string",
                    "enum": [
                      "C"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-224>"
                  }
                },
                "x-parser-schema-id": "LiquidTemperatureSpRequestFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "LiquidTemperatureSpRequestMetadata"
          },
          "x-parser-unique-object-id": "liquidTemperatureSpRequest",
          "x-parser-message-name": "LiquidTemperatureSpRequestMsg"
        },
        "genericPoint": "$ref:$.channels.bessMetadata.messages.genericPoint"
      },
      "x-parser-unique-object-id": "cduMetadata"
    },
    "cduIntegrationValue": {
      "address": "BMS/v1/{integration}/Value/CDU/{pointType}/{tagPath}",
      "description": "Values published by integrations for CDU control points.\nSubscribe to `cduMetadata` first and read `integration` for the exact topic.\n\n**MQTT wildcard examples**\n- All integration CDU values: `BMS/v1/+/Value/CDU/#`\n",
      "parameters": {
        "integration": {
          "description": "Integration identifier."
        },
        "pointType": {
          "enum": [
            "LiquidTemperatureSpRequest"
          ],
          "description": "Integration-published CDU point type."
        },
        "tagPath": {
          "description": "Must match the tagPath from the corresponding BMS metadata exactly."
        }
      },
      "messages": {
        "valueMessage": "$ref:$.channels.rackBmsValue.messages.valueMessage"
      },
      "x-parser-unique-object-id": "cduIntegrationValue"
    },
    "coolingTowerValue": {
      "address": "BMS/v1/PUB/Value/CoolingTower/{pointType}/{tagPath}",
      "description": "Real-time values published by the BMS for CoolingTower points.\n\n**MQTT wildcard examples**\n- All CoolingTower values: `BMS/v1/PUB/Value/CoolingTower/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "LiquidTemperature",
            "LiquidDifferentialPressure",
            "LiquidFlow",
            "LiquidPressure",
            "Status",
            "Available",
            "ValvePosition",
            "PumpSpeed",
            "FanSpeed",
            "DamperPosition",
            "AirTemperature",
            "AirDifferentialPressure",
            "AirRelativeHumidity",
            "AirFlow",
            "AirPressure",
            "LeakDetect",
            "GenericPoint"
          ],
          "description": "BMS-published CoolingTower point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "valueMessage": "$ref:$.channels.rackBmsValue.messages.valueMessage"
      },
      "x-parser-unique-object-id": "coolingTowerValue"
    },
    "coolingTowerMetadata": {
      "address": "BMS/v1/PUB/Metadata/CoolingTower/{pointType}/{tagPath}",
      "description": "BMS-published metadata for CoolingTower points.\n\n**MQTT wildcard examples**\n- All CoolingTower metadata: `BMS/v1/PUB/Metadata/CoolingTower/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "LiquidTemperature",
            "LiquidDifferentialPressure",
            "LiquidFlow",
            "LiquidPressure",
            "Status",
            "Available",
            "ValvePosition",
            "PumpSpeed",
            "FanSpeed",
            "DamperPosition",
            "AirTemperature",
            "AirDifferentialPressure",
            "AirRelativeHumidity",
            "AirFlow",
            "AirPressure",
            "LeakDetect",
            "GenericPoint"
          ],
          "description": "CoolingTower point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "liquidTemperature": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidTemperature.payload.allOf[1]",
              {
                "type": "object",
                "properties": {
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "CoolingTower"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-232>"
                  }
                },
                "x-parser-schema-id": "CoolingTowerObjectTypeFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CoolingTowerLiquidTemperatureMetadata"
          },
          "x-parser-unique-object-id": "liquidTemperature",
          "x-parser-message-name": "CoolingTowerLiquidTemperatureMsg"
        },
        "liquidDifferentialPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[1]",
              "$ref:$.channels.coolingTowerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CoolingTowerLiquidDifferentialPressureMetadata"
          },
          "x-parser-unique-object-id": "liquidDifferentialPressure",
          "x-parser-message-name": "CoolingTowerLiquidDifferentialPressureMsg"
        },
        "liquidFlow": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidFlow.payload.allOf[1]",
              "$ref:$.channels.coolingTowerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CoolingTowerLiquidFlowMetadata"
          },
          "x-parser-unique-object-id": "liquidFlow",
          "x-parser-message-name": "CoolingTowerLiquidFlowMsg"
        },
        "liquidPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidPressure.payload.allOf[1]",
              "$ref:$.channels.coolingTowerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CoolingTowerLiquidPressureMetadata"
          },
          "x-parser-unique-object-id": "liquidPressure",
          "x-parser-message-name": "CoolingTowerLiquidPressureMsg"
        },
        "status": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[1]",
              "$ref:$.channels.coolingTowerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CoolingTowerStatusMetadata"
          },
          "x-parser-unique-object-id": "status",
          "x-parser-message-name": "CoolingTowerStatusMsg"
        },
        "available": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.available.payload.allOf[1]",
              "$ref:$.channels.coolingTowerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CoolingTowerAvailableMetadata"
          },
          "x-parser-unique-object-id": "available",
          "x-parser-message-name": "CoolingTowerAvailableMsg"
        },
        "valvePosition": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.valvePosition.payload.allOf[1]",
              "$ref:$.channels.coolingTowerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CoolingTowerValvePositionMetadata"
          },
          "x-parser-unique-object-id": "valvePosition",
          "x-parser-message-name": "CoolingTowerValvePositionMsg"
        },
        "pumpSpeed": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.pumpSpeed.payload.allOf[1]",
              "$ref:$.channels.coolingTowerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CoolingTowerPumpSpeedMetadata"
          },
          "x-parser-unique-object-id": "pumpSpeed",
          "x-parser-message-name": "CoolingTowerPumpSpeedMsg"
        },
        "fanSpeed": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.fanSpeed.payload.allOf[1]",
              "$ref:$.channels.coolingTowerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CoolingTowerFanSpeedMetadata"
          },
          "x-parser-unique-object-id": "fanSpeed",
          "x-parser-message-name": "CoolingTowerFanSpeedMsg"
        },
        "damperPosition": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.damperPosition.payload.allOf[1]",
              "$ref:$.channels.coolingTowerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CoolingTowerDamperPositionMetadata"
          },
          "x-parser-unique-object-id": "damperPosition",
          "x-parser-message-name": "CoolingTowerDamperPositionMsg"
        },
        "airTemperature": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airTemperature.payload.allOf[1]",
              "$ref:$.channels.coolingTowerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CoolingTowerAirTemperatureMetadata"
          },
          "x-parser-unique-object-id": "airTemperature",
          "x-parser-message-name": "CoolingTowerAirTemperatureMsg"
        },
        "airDifferentialPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airDifferentialPressure.payload.allOf[1]",
              "$ref:$.channels.coolingTowerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CoolingTowerAirDifferentialPressureMetadata"
          },
          "x-parser-unique-object-id": "airDifferentialPressure",
          "x-parser-message-name": "CoolingTowerAirDifferentialPressureMsg"
        },
        "airRelativeHumidity": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airRelativeHumidity.payload.allOf[1]",
              "$ref:$.channels.coolingTowerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CoolingTowerAirRelativeHumidityMetadata"
          },
          "x-parser-unique-object-id": "airRelativeHumidity",
          "x-parser-message-name": "CoolingTowerAirRelativeHumidityMsg"
        },
        "airFlow": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airFlow.payload.allOf[1]",
              "$ref:$.channels.coolingTowerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CoolingTowerAirFlowMetadata"
          },
          "x-parser-unique-object-id": "airFlow",
          "x-parser-message-name": "CoolingTowerAirFlowMsg"
        },
        "airPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airPressure.payload.allOf[1]",
              "$ref:$.channels.coolingTowerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CoolingTowerAirPressureMetadata"
          },
          "x-parser-unique-object-id": "airPressure",
          "x-parser-message-name": "CoolingTowerAirPressureMsg"
        },
        "leakDetect": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.leakDetect.payload.allOf[1]",
              "$ref:$.channels.coolingTowerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CoolingTowerLeakDetectMetadata"
          },
          "x-parser-unique-object-id": "leakDetect",
          "x-parser-message-name": "CoolingTowerLeakDetectMsg"
        },
        "genericPoint": "$ref:$.channels.bessMetadata.messages.genericPoint"
      },
      "x-parser-unique-object-id": "coolingTowerMetadata"
    },
    "hxValue": {
      "address": "BMS/v1/PUB/Value/HX/{pointType}/{tagPath}",
      "description": "Real-time values published by the BMS for HX points.\n\n**MQTT wildcard examples**\n- All HX values: `BMS/v1/PUB/Value/HX/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "LiquidTemperature",
            "LiquidDifferentialPressure",
            "LiquidFlow",
            "LiquidPressure",
            "Status",
            "Available",
            "ValvePosition",
            "PumpSpeed",
            "FanSpeed",
            "DamperPosition",
            "AirTemperature",
            "AirDifferentialPressure",
            "AirRelativeHumidity",
            "AirFlow",
            "AirPressure",
            "LeakDetect",
            "GenericPoint"
          ],
          "description": "BMS-published HX point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "valueMessage": "$ref:$.channels.rackBmsValue.messages.valueMessage"
      },
      "x-parser-unique-object-id": "hxValue"
    },
    "hxMetadata": {
      "address": "BMS/v1/PUB/Metadata/HX/{pointType}/{tagPath}",
      "description": "BMS-published metadata for HX points.\n\n**MQTT wildcard examples**\n- All HX metadata: `BMS/v1/PUB/Metadata/HX/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "LiquidTemperature",
            "LiquidDifferentialPressure",
            "LiquidFlow",
            "LiquidPressure",
            "Status",
            "Available",
            "ValvePosition",
            "PumpSpeed",
            "FanSpeed",
            "DamperPosition",
            "AirTemperature",
            "AirDifferentialPressure",
            "AirRelativeHumidity",
            "AirFlow",
            "AirPressure",
            "LeakDetect",
            "GenericPoint"
          ],
          "description": "HX point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "liquidTemperature": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidTemperature.payload.allOf[1]",
              {
                "type": "object",
                "properties": {
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "HX"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-237>"
                  }
                },
                "x-parser-schema-id": "HXObjectTypeFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "HXLiquidTemperatureMetadata"
          },
          "x-parser-unique-object-id": "liquidTemperature",
          "x-parser-message-name": "HXLiquidTemperatureMsg"
        },
        "liquidDifferentialPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[1]",
              "$ref:$.channels.hxMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "HXLiquidDifferentialPressureMetadata"
          },
          "x-parser-unique-object-id": "liquidDifferentialPressure",
          "x-parser-message-name": "HXLiquidDifferentialPressureMsg"
        },
        "liquidFlow": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidFlow.payload.allOf[1]",
              "$ref:$.channels.hxMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "HXLiquidFlowMetadata"
          },
          "x-parser-unique-object-id": "liquidFlow",
          "x-parser-message-name": "HXLiquidFlowMsg"
        },
        "liquidPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidPressure.payload.allOf[1]",
              "$ref:$.channels.hxMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "HXLiquidPressureMetadata"
          },
          "x-parser-unique-object-id": "liquidPressure",
          "x-parser-message-name": "HXLiquidPressureMsg"
        },
        "status": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[1]",
              "$ref:$.channels.hxMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "HXStatusMetadata"
          },
          "x-parser-unique-object-id": "status",
          "x-parser-message-name": "HXStatusMsg"
        },
        "available": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.available.payload.allOf[1]",
              "$ref:$.channels.hxMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "HXAvailableMetadata"
          },
          "x-parser-unique-object-id": "available",
          "x-parser-message-name": "HXAvailableMsg"
        },
        "valvePosition": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.valvePosition.payload.allOf[1]",
              "$ref:$.channels.hxMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "HXValvePositionMetadata"
          },
          "x-parser-unique-object-id": "valvePosition",
          "x-parser-message-name": "HXValvePositionMsg"
        },
        "pumpSpeed": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.pumpSpeed.payload.allOf[1]",
              "$ref:$.channels.hxMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "HXPumpSpeedMetadata"
          },
          "x-parser-unique-object-id": "pumpSpeed",
          "x-parser-message-name": "HXPumpSpeedMsg"
        },
        "fanSpeed": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.fanSpeed.payload.allOf[1]",
              "$ref:$.channels.hxMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "HXFanSpeedMetadata"
          },
          "x-parser-unique-object-id": "fanSpeed",
          "x-parser-message-name": "HXFanSpeedMsg"
        },
        "damperPosition": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.damperPosition.payload.allOf[1]",
              "$ref:$.channels.hxMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "HXDamperPositionMetadata"
          },
          "x-parser-unique-object-id": "damperPosition",
          "x-parser-message-name": "HXDamperPositionMsg"
        },
        "airTemperature": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airTemperature.payload.allOf[1]",
              "$ref:$.channels.hxMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "HXAirTemperatureMetadata"
          },
          "x-parser-unique-object-id": "airTemperature",
          "x-parser-message-name": "HXAirTemperatureMsg"
        },
        "airDifferentialPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airDifferentialPressure.payload.allOf[1]",
              "$ref:$.channels.hxMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "HXAirDifferentialPressureMetadata"
          },
          "x-parser-unique-object-id": "airDifferentialPressure",
          "x-parser-message-name": "HXAirDifferentialPressureMsg"
        },
        "airRelativeHumidity": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airRelativeHumidity.payload.allOf[1]",
              "$ref:$.channels.hxMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "HXAirRelativeHumidityMetadata"
          },
          "x-parser-unique-object-id": "airRelativeHumidity",
          "x-parser-message-name": "HXAirRelativeHumidityMsg"
        },
        "airFlow": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airFlow.payload.allOf[1]",
              "$ref:$.channels.hxMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "HXAirFlowMetadata"
          },
          "x-parser-unique-object-id": "airFlow",
          "x-parser-message-name": "HXAirFlowMsg"
        },
        "airPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airPressure.payload.allOf[1]",
              "$ref:$.channels.hxMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "HXAirPressureMetadata"
          },
          "x-parser-unique-object-id": "airPressure",
          "x-parser-message-name": "HXAirPressureMsg"
        },
        "leakDetect": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.leakDetect.payload.allOf[1]",
              "$ref:$.channels.hxMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "HXLeakDetectMetadata"
          },
          "x-parser-unique-object-id": "leakDetect",
          "x-parser-message-name": "HXLeakDetectMsg"
        },
        "genericPoint": "$ref:$.channels.bessMetadata.messages.genericPoint"
      },
      "x-parser-unique-object-id": "hxMetadata"
    },
    "crahValue": {
      "address": "BMS/v1/PUB/Value/CRAH/{pointType}/{tagPath}",
      "description": "Real-time values published by the BMS for CRAH points.\n\n**MQTT wildcard examples**\n- All CRAH values: `BMS/v1/PUB/Value/CRAH/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "LiquidTemperature",
            "LiquidDifferentialPressure",
            "LiquidFlow",
            "LiquidPressure",
            "Status",
            "Available",
            "ValvePosition",
            "PumpSpeed",
            "FanSpeed",
            "DamperPosition",
            "AirTemperature",
            "AirDifferentialPressure",
            "AirRelativeHumidity",
            "AirFlow",
            "AirPressure",
            "LeakDetect",
            "GenericPoint"
          ],
          "description": "BMS-published CRAH point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "valueMessage": "$ref:$.channels.rackBmsValue.messages.valueMessage"
      },
      "x-parser-unique-object-id": "crahValue"
    },
    "crahMetadata": {
      "address": "BMS/v1/PUB/Metadata/CRAH/{pointType}/{tagPath}",
      "description": "BMS-published metadata for CRAH points.\n\n**MQTT wildcard examples**\n- All CRAH metadata: `BMS/v1/PUB/Metadata/CRAH/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "LiquidTemperature",
            "LiquidDifferentialPressure",
            "LiquidFlow",
            "LiquidPressure",
            "Status",
            "Available",
            "ValvePosition",
            "PumpSpeed",
            "FanSpeed",
            "DamperPosition",
            "AirTemperature",
            "AirDifferentialPressure",
            "AirRelativeHumidity",
            "AirFlow",
            "AirPressure",
            "LeakDetect",
            "GenericPoint"
          ],
          "description": "CRAH point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "liquidTemperature": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidTemperature.payload.allOf[1]",
              {
                "type": "object",
                "properties": {
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "CRAH"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-242>"
                  }
                },
                "x-parser-schema-id": "CRAHObjectTypeFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRAHLiquidTemperatureMetadata"
          },
          "x-parser-unique-object-id": "liquidTemperature",
          "x-parser-message-name": "CRAHLiquidTemperatureMsg"
        },
        "liquidDifferentialPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[1]",
              "$ref:$.channels.crahMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRAHLiquidDifferentialPressureMetadata"
          },
          "x-parser-unique-object-id": "liquidDifferentialPressure",
          "x-parser-message-name": "CRAHLiquidDifferentialPressureMsg"
        },
        "liquidFlow": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidFlow.payload.allOf[1]",
              "$ref:$.channels.crahMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRAHLiquidFlowMetadata"
          },
          "x-parser-unique-object-id": "liquidFlow",
          "x-parser-message-name": "CRAHLiquidFlowMsg"
        },
        "liquidPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidPressure.payload.allOf[1]",
              "$ref:$.channels.crahMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRAHLiquidPressureMetadata"
          },
          "x-parser-unique-object-id": "liquidPressure",
          "x-parser-message-name": "CRAHLiquidPressureMsg"
        },
        "status": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[1]",
              "$ref:$.channels.crahMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRAHStatusMetadata"
          },
          "x-parser-unique-object-id": "status",
          "x-parser-message-name": "CRAHStatusMsg"
        },
        "available": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.available.payload.allOf[1]",
              "$ref:$.channels.crahMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRAHAvailableMetadata"
          },
          "x-parser-unique-object-id": "available",
          "x-parser-message-name": "CRAHAvailableMsg"
        },
        "valvePosition": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.valvePosition.payload.allOf[1]",
              "$ref:$.channels.crahMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRAHValvePositionMetadata"
          },
          "x-parser-unique-object-id": "valvePosition",
          "x-parser-message-name": "CRAHValvePositionMsg"
        },
        "pumpSpeed": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.pumpSpeed.payload.allOf[1]",
              "$ref:$.channels.crahMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRAHPumpSpeedMetadata"
          },
          "x-parser-unique-object-id": "pumpSpeed",
          "x-parser-message-name": "CRAHPumpSpeedMsg"
        },
        "fanSpeed": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.fanSpeed.payload.allOf[1]",
              "$ref:$.channels.crahMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRAHFanSpeedMetadata"
          },
          "x-parser-unique-object-id": "fanSpeed",
          "x-parser-message-name": "CRAHFanSpeedMsg"
        },
        "damperPosition": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.damperPosition.payload.allOf[1]",
              "$ref:$.channels.crahMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRAHDamperPositionMetadata"
          },
          "x-parser-unique-object-id": "damperPosition",
          "x-parser-message-name": "CRAHDamperPositionMsg"
        },
        "airTemperature": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airTemperature.payload.allOf[1]",
              "$ref:$.channels.crahMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRAHAirTemperatureMetadata"
          },
          "x-parser-unique-object-id": "airTemperature",
          "x-parser-message-name": "CRAHAirTemperatureMsg"
        },
        "airDifferentialPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airDifferentialPressure.payload.allOf[1]",
              "$ref:$.channels.crahMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRAHAirDifferentialPressureMetadata"
          },
          "x-parser-unique-object-id": "airDifferentialPressure",
          "x-parser-message-name": "CRAHAirDifferentialPressureMsg"
        },
        "airRelativeHumidity": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airRelativeHumidity.payload.allOf[1]",
              "$ref:$.channels.crahMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRAHAirRelativeHumidityMetadata"
          },
          "x-parser-unique-object-id": "airRelativeHumidity",
          "x-parser-message-name": "CRAHAirRelativeHumidityMsg"
        },
        "airFlow": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airFlow.payload.allOf[1]",
              "$ref:$.channels.crahMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRAHAirFlowMetadata"
          },
          "x-parser-unique-object-id": "airFlow",
          "x-parser-message-name": "CRAHAirFlowMsg"
        },
        "airPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airPressure.payload.allOf[1]",
              "$ref:$.channels.crahMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRAHAirPressureMetadata"
          },
          "x-parser-unique-object-id": "airPressure",
          "x-parser-message-name": "CRAHAirPressureMsg"
        },
        "leakDetect": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.leakDetect.payload.allOf[1]",
              "$ref:$.channels.crahMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRAHLeakDetectMetadata"
          },
          "x-parser-unique-object-id": "leakDetect",
          "x-parser-message-name": "CRAHLeakDetectMsg"
        },
        "genericPoint": "$ref:$.channels.bessMetadata.messages.genericPoint"
      },
      "x-parser-unique-object-id": "crahMetadata"
    },
    "cracValue": {
      "address": "BMS/v1/PUB/Value/CRAC/{pointType}/{tagPath}",
      "description": "Real-time values published by the BMS for CRAC points.\n\n**MQTT wildcard examples**\n- All CRAC values: `BMS/v1/PUB/Value/CRAC/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "LiquidTemperature",
            "LiquidDifferentialPressure",
            "LiquidFlow",
            "LiquidPressure",
            "Status",
            "Available",
            "ValvePosition",
            "PumpSpeed",
            "FanSpeed",
            "DamperPosition",
            "AirTemperature",
            "AirDifferentialPressure",
            "AirRelativeHumidity",
            "AirFlow",
            "AirPressure",
            "LeakDetect",
            "GenericPoint"
          ],
          "description": "BMS-published CRAC point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "valueMessage": "$ref:$.channels.rackBmsValue.messages.valueMessage"
      },
      "x-parser-unique-object-id": "cracValue"
    },
    "cracMetadata": {
      "address": "BMS/v1/PUB/Metadata/CRAC/{pointType}/{tagPath}",
      "description": "BMS-published metadata for CRAC points.\n\n**MQTT wildcard examples**\n- All CRAC metadata: `BMS/v1/PUB/Metadata/CRAC/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "LiquidTemperature",
            "LiquidDifferentialPressure",
            "LiquidFlow",
            "LiquidPressure",
            "Status",
            "Available",
            "ValvePosition",
            "PumpSpeed",
            "FanSpeed",
            "DamperPosition",
            "AirTemperature",
            "AirDifferentialPressure",
            "AirRelativeHumidity",
            "AirFlow",
            "AirPressure",
            "LeakDetect",
            "GenericPoint"
          ],
          "description": "CRAC point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "liquidTemperature": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidTemperature.payload.allOf[1]",
              {
                "type": "object",
                "properties": {
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "CRAC"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-247>"
                  }
                },
                "x-parser-schema-id": "CRACObjectTypeFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRACLiquidTemperatureMetadata"
          },
          "x-parser-unique-object-id": "liquidTemperature",
          "x-parser-message-name": "CRACLiquidTemperatureMsg"
        },
        "liquidDifferentialPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[1]",
              "$ref:$.channels.cracMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRACLiquidDifferentialPressureMetadata"
          },
          "x-parser-unique-object-id": "liquidDifferentialPressure",
          "x-parser-message-name": "CRACLiquidDifferentialPressureMsg"
        },
        "liquidFlow": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidFlow.payload.allOf[1]",
              "$ref:$.channels.cracMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRACLiquidFlowMetadata"
          },
          "x-parser-unique-object-id": "liquidFlow",
          "x-parser-message-name": "CRACLiquidFlowMsg"
        },
        "liquidPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidPressure.payload.allOf[1]",
              "$ref:$.channels.cracMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRACLiquidPressureMetadata"
          },
          "x-parser-unique-object-id": "liquidPressure",
          "x-parser-message-name": "CRACLiquidPressureMsg"
        },
        "status": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[1]",
              "$ref:$.channels.cracMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRACStatusMetadata"
          },
          "x-parser-unique-object-id": "status",
          "x-parser-message-name": "CRACStatusMsg"
        },
        "available": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.available.payload.allOf[1]",
              "$ref:$.channels.cracMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRACAvailableMetadata"
          },
          "x-parser-unique-object-id": "available",
          "x-parser-message-name": "CRACAvailableMsg"
        },
        "valvePosition": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.valvePosition.payload.allOf[1]",
              "$ref:$.channels.cracMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRACValvePositionMetadata"
          },
          "x-parser-unique-object-id": "valvePosition",
          "x-parser-message-name": "CRACValvePositionMsg"
        },
        "pumpSpeed": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.pumpSpeed.payload.allOf[1]",
              "$ref:$.channels.cracMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRACPumpSpeedMetadata"
          },
          "x-parser-unique-object-id": "pumpSpeed",
          "x-parser-message-name": "CRACPumpSpeedMsg"
        },
        "fanSpeed": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.fanSpeed.payload.allOf[1]",
              "$ref:$.channels.cracMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRACFanSpeedMetadata"
          },
          "x-parser-unique-object-id": "fanSpeed",
          "x-parser-message-name": "CRACFanSpeedMsg"
        },
        "damperPosition": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.damperPosition.payload.allOf[1]",
              "$ref:$.channels.cracMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRACDamperPositionMetadata"
          },
          "x-parser-unique-object-id": "damperPosition",
          "x-parser-message-name": "CRACDamperPositionMsg"
        },
        "airTemperature": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airTemperature.payload.allOf[1]",
              "$ref:$.channels.cracMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRACAirTemperatureMetadata"
          },
          "x-parser-unique-object-id": "airTemperature",
          "x-parser-message-name": "CRACAirTemperatureMsg"
        },
        "airDifferentialPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airDifferentialPressure.payload.allOf[1]",
              "$ref:$.channels.cracMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRACAirDifferentialPressureMetadata"
          },
          "x-parser-unique-object-id": "airDifferentialPressure",
          "x-parser-message-name": "CRACAirDifferentialPressureMsg"
        },
        "airRelativeHumidity": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airRelativeHumidity.payload.allOf[1]",
              "$ref:$.channels.cracMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRACAirRelativeHumidityMetadata"
          },
          "x-parser-unique-object-id": "airRelativeHumidity",
          "x-parser-message-name": "CRACAirRelativeHumidityMsg"
        },
        "airFlow": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airFlow.payload.allOf[1]",
              "$ref:$.channels.cracMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRACAirFlowMetadata"
          },
          "x-parser-unique-object-id": "airFlow",
          "x-parser-message-name": "CRACAirFlowMsg"
        },
        "airPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airPressure.payload.allOf[1]",
              "$ref:$.channels.cracMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRACAirPressureMetadata"
          },
          "x-parser-unique-object-id": "airPressure",
          "x-parser-message-name": "CRACAirPressureMsg"
        },
        "leakDetect": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.leakDetect.payload.allOf[1]",
              "$ref:$.channels.cracMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "CRACLeakDetectMetadata"
          },
          "x-parser-unique-object-id": "leakDetect",
          "x-parser-message-name": "CRACLeakDetectMsg"
        },
        "genericPoint": "$ref:$.channels.bessMetadata.messages.genericPoint"
      },
      "x-parser-unique-object-id": "cracMetadata"
    },
    "ahuValue": {
      "address": "BMS/v1/PUB/Value/AHU/{pointType}/{tagPath}",
      "description": "Real-time values published by the BMS for AHU points.\n\n**MQTT wildcard examples**\n- All AHU values: `BMS/v1/PUB/Value/AHU/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "LiquidTemperature",
            "LiquidDifferentialPressure",
            "LiquidFlow",
            "LiquidPressure",
            "Status",
            "Available",
            "ValvePosition",
            "PumpSpeed",
            "FanSpeed",
            "DamperPosition",
            "AirTemperature",
            "AirDifferentialPressure",
            "AirRelativeHumidity",
            "AirFlow",
            "AirPressure",
            "LeakDetect",
            "GenericPoint"
          ],
          "description": "BMS-published AHU point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "valueMessage": "$ref:$.channels.rackBmsValue.messages.valueMessage"
      },
      "x-parser-unique-object-id": "ahuValue"
    },
    "ahuMetadata": {
      "address": "BMS/v1/PUB/Metadata/AHU/{pointType}/{tagPath}",
      "description": "BMS-published metadata for AHU points.\n\n**MQTT wildcard examples**\n- All AHU metadata: `BMS/v1/PUB/Metadata/AHU/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "LiquidTemperature",
            "LiquidDifferentialPressure",
            "LiquidFlow",
            "LiquidPressure",
            "Status",
            "Available",
            "ValvePosition",
            "PumpSpeed",
            "FanSpeed",
            "DamperPosition",
            "AirTemperature",
            "AirDifferentialPressure",
            "AirRelativeHumidity",
            "AirFlow",
            "AirPressure",
            "LeakDetect",
            "GenericPoint"
          ],
          "description": "AHU point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "liquidTemperature": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidTemperature.payload.allOf[1]",
              {
                "type": "object",
                "properties": {
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "AHU"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-252>"
                  }
                },
                "x-parser-schema-id": "AHUObjectTypeFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "AHULiquidTemperatureMetadata"
          },
          "x-parser-unique-object-id": "liquidTemperature",
          "x-parser-message-name": "AHULiquidTemperatureMsg"
        },
        "liquidDifferentialPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[1]",
              "$ref:$.channels.ahuMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "AHULiquidDifferentialPressureMetadata"
          },
          "x-parser-unique-object-id": "liquidDifferentialPressure",
          "x-parser-message-name": "AHULiquidDifferentialPressureMsg"
        },
        "liquidFlow": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidFlow.payload.allOf[1]",
              "$ref:$.channels.ahuMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "AHULiquidFlowMetadata"
          },
          "x-parser-unique-object-id": "liquidFlow",
          "x-parser-message-name": "AHULiquidFlowMsg"
        },
        "liquidPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidPressure.payload.allOf[1]",
              "$ref:$.channels.ahuMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "AHULiquidPressureMetadata"
          },
          "x-parser-unique-object-id": "liquidPressure",
          "x-parser-message-name": "AHULiquidPressureMsg"
        },
        "status": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[1]",
              "$ref:$.channels.ahuMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "AHUStatusMetadata"
          },
          "x-parser-unique-object-id": "status",
          "x-parser-message-name": "AHUStatusMsg"
        },
        "available": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.available.payload.allOf[1]",
              "$ref:$.channels.ahuMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "AHUAvailableMetadata"
          },
          "x-parser-unique-object-id": "available",
          "x-parser-message-name": "AHUAvailableMsg"
        },
        "valvePosition": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.valvePosition.payload.allOf[1]",
              "$ref:$.channels.ahuMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "AHUValvePositionMetadata"
          },
          "x-parser-unique-object-id": "valvePosition",
          "x-parser-message-name": "AHUValvePositionMsg"
        },
        "pumpSpeed": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.pumpSpeed.payload.allOf[1]",
              "$ref:$.channels.ahuMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "AHUPumpSpeedMetadata"
          },
          "x-parser-unique-object-id": "pumpSpeed",
          "x-parser-message-name": "AHUPumpSpeedMsg"
        },
        "fanSpeed": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.fanSpeed.payload.allOf[1]",
              "$ref:$.channels.ahuMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "AHUFanSpeedMetadata"
          },
          "x-parser-unique-object-id": "fanSpeed",
          "x-parser-message-name": "AHUFanSpeedMsg"
        },
        "damperPosition": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.damperPosition.payload.allOf[1]",
              "$ref:$.channels.ahuMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "AHUDamperPositionMetadata"
          },
          "x-parser-unique-object-id": "damperPosition",
          "x-parser-message-name": "AHUDamperPositionMsg"
        },
        "airTemperature": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airTemperature.payload.allOf[1]",
              "$ref:$.channels.ahuMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "AHUAirTemperatureMetadata"
          },
          "x-parser-unique-object-id": "airTemperature",
          "x-parser-message-name": "AHUAirTemperatureMsg"
        },
        "airDifferentialPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airDifferentialPressure.payload.allOf[1]",
              "$ref:$.channels.ahuMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "AHUAirDifferentialPressureMetadata"
          },
          "x-parser-unique-object-id": "airDifferentialPressure",
          "x-parser-message-name": "AHUAirDifferentialPressureMsg"
        },
        "airRelativeHumidity": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airRelativeHumidity.payload.allOf[1]",
              "$ref:$.channels.ahuMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "AHUAirRelativeHumidityMetadata"
          },
          "x-parser-unique-object-id": "airRelativeHumidity",
          "x-parser-message-name": "AHUAirRelativeHumidityMsg"
        },
        "airFlow": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airFlow.payload.allOf[1]",
              "$ref:$.channels.ahuMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "AHUAirFlowMetadata"
          },
          "x-parser-unique-object-id": "airFlow",
          "x-parser-message-name": "AHUAirFlowMsg"
        },
        "airPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airPressure.payload.allOf[1]",
              "$ref:$.channels.ahuMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "AHUAirPressureMetadata"
          },
          "x-parser-unique-object-id": "airPressure",
          "x-parser-message-name": "AHUAirPressureMsg"
        },
        "leakDetect": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.leakDetect.payload.allOf[1]",
              "$ref:$.channels.ahuMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "AHULeakDetectMetadata"
          },
          "x-parser-unique-object-id": "leakDetect",
          "x-parser-message-name": "AHULeakDetectMsg"
        },
        "genericPoint": "$ref:$.channels.bessMetadata.messages.genericPoint"
      },
      "x-parser-unique-object-id": "ahuMetadata"
    },
    "chillerValue": {
      "address": "BMS/v1/PUB/Value/Chiller/{pointType}/{tagPath}",
      "description": "Real-time values published by the BMS for Chiller points.\n\n**MQTT wildcard examples**\n- All Chiller values: `BMS/v1/PUB/Value/Chiller/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "LiquidTemperature",
            "LiquidDifferentialPressure",
            "LiquidFlow",
            "LiquidPressure",
            "Status",
            "Available",
            "ValvePosition",
            "PumpSpeed",
            "FanSpeed",
            "DamperPosition",
            "AirTemperature",
            "AirDifferentialPressure",
            "AirRelativeHumidity",
            "AirFlow",
            "AirPressure",
            "LeakDetect",
            "GenericPoint"
          ],
          "description": "BMS-published Chiller point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "valueMessage": "$ref:$.channels.rackBmsValue.messages.valueMessage"
      },
      "x-parser-unique-object-id": "chillerValue"
    },
    "chillerMetadata": {
      "address": "BMS/v1/PUB/Metadata/Chiller/{pointType}/{tagPath}",
      "description": "BMS-published metadata for Chiller points.\n\n**MQTT wildcard examples**\n- All Chiller metadata: `BMS/v1/PUB/Metadata/Chiller/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "LiquidTemperature",
            "LiquidDifferentialPressure",
            "LiquidFlow",
            "LiquidPressure",
            "Status",
            "Available",
            "ValvePosition",
            "PumpSpeed",
            "FanSpeed",
            "DamperPosition",
            "AirTemperature",
            "AirDifferentialPressure",
            "AirRelativeHumidity",
            "AirFlow",
            "AirPressure",
            "LeakDetect",
            "GenericPoint"
          ],
          "description": "Chiller point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "liquidTemperature": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidTemperature.payload.allOf[1]",
              {
                "type": "object",
                "properties": {
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "Chiller"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-257>"
                  }
                },
                "x-parser-schema-id": "ChillerObjectTypeFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "ChillerLiquidTemperatureMetadata"
          },
          "x-parser-unique-object-id": "liquidTemperature",
          "x-parser-message-name": "ChillerLiquidTemperatureMsg"
        },
        "liquidDifferentialPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[1]",
              "$ref:$.channels.chillerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "ChillerLiquidDifferentialPressureMetadata"
          },
          "x-parser-unique-object-id": "liquidDifferentialPressure",
          "x-parser-message-name": "ChillerLiquidDifferentialPressureMsg"
        },
        "liquidFlow": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidFlow.payload.allOf[1]",
              "$ref:$.channels.chillerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "ChillerLiquidFlowMetadata"
          },
          "x-parser-unique-object-id": "liquidFlow",
          "x-parser-message-name": "ChillerLiquidFlowMsg"
        },
        "liquidPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidPressure.payload.allOf[1]",
              "$ref:$.channels.chillerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "ChillerLiquidPressureMetadata"
          },
          "x-parser-unique-object-id": "liquidPressure",
          "x-parser-message-name": "ChillerLiquidPressureMsg"
        },
        "status": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[1]",
              "$ref:$.channels.chillerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "ChillerStatusMetadata"
          },
          "x-parser-unique-object-id": "status",
          "x-parser-message-name": "ChillerStatusMsg"
        },
        "available": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.available.payload.allOf[1]",
              "$ref:$.channels.chillerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "ChillerAvailableMetadata"
          },
          "x-parser-unique-object-id": "available",
          "x-parser-message-name": "ChillerAvailableMsg"
        },
        "valvePosition": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.valvePosition.payload.allOf[1]",
              "$ref:$.channels.chillerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "ChillerValvePositionMetadata"
          },
          "x-parser-unique-object-id": "valvePosition",
          "x-parser-message-name": "ChillerValvePositionMsg"
        },
        "pumpSpeed": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.pumpSpeed.payload.allOf[1]",
              "$ref:$.channels.chillerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "ChillerPumpSpeedMetadata"
          },
          "x-parser-unique-object-id": "pumpSpeed",
          "x-parser-message-name": "ChillerPumpSpeedMsg"
        },
        "fanSpeed": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.fanSpeed.payload.allOf[1]",
              "$ref:$.channels.chillerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "ChillerFanSpeedMetadata"
          },
          "x-parser-unique-object-id": "fanSpeed",
          "x-parser-message-name": "ChillerFanSpeedMsg"
        },
        "damperPosition": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.damperPosition.payload.allOf[1]",
              "$ref:$.channels.chillerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "ChillerDamperPositionMetadata"
          },
          "x-parser-unique-object-id": "damperPosition",
          "x-parser-message-name": "ChillerDamperPositionMsg"
        },
        "airTemperature": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airTemperature.payload.allOf[1]",
              "$ref:$.channels.chillerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "ChillerAirTemperatureMetadata"
          },
          "x-parser-unique-object-id": "airTemperature",
          "x-parser-message-name": "ChillerAirTemperatureMsg"
        },
        "airDifferentialPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airDifferentialPressure.payload.allOf[1]",
              "$ref:$.channels.chillerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "ChillerAirDifferentialPressureMetadata"
          },
          "x-parser-unique-object-id": "airDifferentialPressure",
          "x-parser-message-name": "ChillerAirDifferentialPressureMsg"
        },
        "airRelativeHumidity": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airRelativeHumidity.payload.allOf[1]",
              "$ref:$.channels.chillerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "ChillerAirRelativeHumidityMetadata"
          },
          "x-parser-unique-object-id": "airRelativeHumidity",
          "x-parser-message-name": "ChillerAirRelativeHumidityMsg"
        },
        "airFlow": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airFlow.payload.allOf[1]",
              "$ref:$.channels.chillerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "ChillerAirFlowMetadata"
          },
          "x-parser-unique-object-id": "airFlow",
          "x-parser-message-name": "ChillerAirFlowMsg"
        },
        "airPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airPressure.payload.allOf[1]",
              "$ref:$.channels.chillerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "ChillerAirPressureMetadata"
          },
          "x-parser-unique-object-id": "airPressure",
          "x-parser-message-name": "ChillerAirPressureMsg"
        },
        "leakDetect": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.leakDetect.payload.allOf[1]",
              "$ref:$.channels.chillerMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "ChillerLeakDetectMetadata"
          },
          "x-parser-unique-object-id": "leakDetect",
          "x-parser-message-name": "ChillerLeakDetectMsg"
        },
        "genericPoint": "$ref:$.channels.bessMetadata.messages.genericPoint"
      },
      "x-parser-unique-object-id": "chillerMetadata"
    },
    "valveValue": {
      "address": "BMS/v1/PUB/Value/Valve/{pointType}/{tagPath}",
      "description": "Real-time values published by the BMS for Valve points.\n\n**MQTT wildcard examples**\n- All Valve values: `BMS/v1/PUB/Value/Valve/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "ValvePosition",
            "Available",
            "GenericPoint"
          ],
          "description": "BMS-published Valve point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "valueMessage": "$ref:$.channels.rackBmsValue.messages.valueMessage"
      },
      "x-parser-unique-object-id": "valveValue"
    },
    "valveMetadata": {
      "address": "BMS/v1/PUB/Metadata/Valve/{pointType}/{tagPath}",
      "description": "BMS-published metadata for Valve points.\n\n**MQTT wildcard examples**\n- All Valve metadata: `BMS/v1/PUB/Metadata/Valve/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "ValvePosition",
            "Available",
            "GenericPoint"
          ],
          "description": "Valve point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "valvePosition": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.valvePosition.payload.allOf[1]",
              {
                "type": "object",
                "properties": {
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "Valve"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-262>"
                  }
                },
                "x-parser-schema-id": "ValveObjectTypeFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "ValveValvePositionMetadata"
          },
          "x-parser-unique-object-id": "valvePosition",
          "x-parser-message-name": "ValveValvePositionMsg"
        },
        "available": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.available.payload.allOf[1]",
              "$ref:$.channels.valveMetadata.messages.valvePosition.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "ValveAvailableMetadata"
          },
          "x-parser-unique-object-id": "available",
          "x-parser-message-name": "ValveAvailableMsg"
        },
        "genericPoint": "$ref:$.channels.bessMetadata.messages.genericPoint"
      },
      "x-parser-unique-object-id": "valveMetadata"
    },
    "pumpValue": {
      "address": "BMS/v1/PUB/Value/Pump/{pointType}/{tagPath}",
      "description": "Real-time values published by the BMS for Pump points.\n\n**MQTT wildcard examples**\n- All Pump values: `BMS/v1/PUB/Value/Pump/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "PumpSpeed",
            "Available",
            "GenericPoint"
          ],
          "description": "BMS-published Pump point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "valueMessage": "$ref:$.channels.rackBmsValue.messages.valueMessage"
      },
      "x-parser-unique-object-id": "pumpValue"
    },
    "pumpMetadata": {
      "address": "BMS/v1/PUB/Metadata/Pump/{pointType}/{tagPath}",
      "description": "BMS-published metadata for Pump points.\n\n**MQTT wildcard examples**\n- All Pump metadata: `BMS/v1/PUB/Metadata/Pump/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "PumpSpeed",
            "Available",
            "GenericPoint"
          ],
          "description": "Pump point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "pumpSpeed": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.pumpSpeed.payload.allOf[1]",
              {
                "type": "object",
                "properties": {
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "Pump"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-267>"
                  }
                },
                "x-parser-schema-id": "PumpObjectTypeFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "PumpPumpSpeedMetadata"
          },
          "x-parser-unique-object-id": "pumpSpeed",
          "x-parser-message-name": "PumpPumpSpeedMsg"
        },
        "available": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.available.payload.allOf[1]",
              "$ref:$.channels.pumpMetadata.messages.pumpSpeed.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "PumpAvailableMetadata"
          },
          "x-parser-unique-object-id": "available",
          "x-parser-message-name": "PumpAvailableMsg"
        },
        "genericPoint": "$ref:$.channels.bessMetadata.messages.genericPoint"
      },
      "x-parser-unique-object-id": "pumpMetadata"
    },
    "fanValue": {
      "address": "BMS/v1/PUB/Value/Fan/{pointType}/{tagPath}",
      "description": "Real-time values published by the BMS for Fan points.\n\n**MQTT wildcard examples**\n- All Fan values: `BMS/v1/PUB/Value/Fan/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "FanSpeed",
            "Available",
            "GenericPoint"
          ],
          "description": "BMS-published Fan point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "valueMessage": "$ref:$.channels.rackBmsValue.messages.valueMessage"
      },
      "x-parser-unique-object-id": "fanValue"
    },
    "fanMetadata": {
      "address": "BMS/v1/PUB/Metadata/Fan/{pointType}/{tagPath}",
      "description": "BMS-published metadata for Fan points.\n\n**MQTT wildcard examples**\n- All Fan metadata: `BMS/v1/PUB/Metadata/Fan/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "FanSpeed",
            "Available",
            "GenericPoint"
          ],
          "description": "Fan point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "fanSpeed": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.fanSpeed.payload.allOf[1]",
              {
                "type": "object",
                "properties": {
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "Fan"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-272>"
                  }
                },
                "x-parser-schema-id": "FanObjectTypeFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "FanFanSpeedMetadata"
          },
          "x-parser-unique-object-id": "fanSpeed",
          "x-parser-message-name": "FanFanSpeedMsg"
        },
        "available": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.available.payload.allOf[1]",
              "$ref:$.channels.fanMetadata.messages.fanSpeed.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "FanAvailableMetadata"
          },
          "x-parser-unique-object-id": "available",
          "x-parser-message-name": "FanAvailableMsg"
        },
        "genericPoint": "$ref:$.channels.bessMetadata.messages.genericPoint"
      },
      "x-parser-unique-object-id": "fanMetadata"
    },
    "damperValue": {
      "address": "BMS/v1/PUB/Value/Damper/{pointType}/{tagPath}",
      "description": "Real-time values published by the BMS for Damper points.\n\n**MQTT wildcard examples**\n- All Damper values: `BMS/v1/PUB/Value/Damper/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "DamperPosition",
            "Available",
            "GenericPoint"
          ],
          "description": "BMS-published Damper point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "valueMessage": "$ref:$.channels.rackBmsValue.messages.valueMessage"
      },
      "x-parser-unique-object-id": "damperValue"
    },
    "damperMetadata": {
      "address": "BMS/v1/PUB/Metadata/Damper/{pointType}/{tagPath}",
      "description": "BMS-published metadata for Damper points.\n\n**MQTT wildcard examples**\n- All Damper metadata: `BMS/v1/PUB/Metadata/Damper/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "DamperPosition",
            "Available",
            "GenericPoint"
          ],
          "description": "Damper point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "damperPosition": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.damperPosition.payload.allOf[1]",
              {
                "type": "object",
                "properties": {
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "Damper"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-277>"
                  }
                },
                "x-parser-schema-id": "DamperObjectTypeFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "DamperDamperPositionMetadata"
          },
          "x-parser-unique-object-id": "damperPosition",
          "x-parser-message-name": "DamperDamperPositionMsg"
        },
        "available": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.available.payload.allOf[1]",
              "$ref:$.channels.damperMetadata.messages.damperPosition.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "DamperAvailableMetadata"
          },
          "x-parser-unique-object-id": "available",
          "x-parser-message-name": "DamperAvailableMsg"
        },
        "genericPoint": "$ref:$.channels.bessMetadata.messages.genericPoint"
      },
      "x-parser-unique-object-id": "damperMetadata"
    },
    "sensorValue": {
      "address": "BMS/v1/PUB/Value/Sensor/{pointType}/{tagPath}",
      "description": "Real-time values published by the BMS for Sensor points.\n\n**MQTT wildcard examples**\n- All Sensor values: `BMS/v1/PUB/Value/Sensor/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "LiquidTemperature",
            "LiquidDifferentialPressure",
            "LiquidFlow",
            "LiquidPressure",
            "AirTemperature",
            "AirDifferentialPressure",
            "AirRelativeHumidity",
            "AirFlow",
            "AirPressure",
            "LeakDetect",
            "Sound",
            "Available",
            "GenericPoint"
          ],
          "description": "BMS-published Sensor point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "valueMessage": "$ref:$.channels.rackBmsValue.messages.valueMessage"
      },
      "x-parser-unique-object-id": "sensorValue"
    },
    "sensorMetadata": {
      "address": "BMS/v1/PUB/Metadata/Sensor/{pointType}/{tagPath}",
      "description": "BMS-published metadata for Sensor points.\n\n**MQTT wildcard examples**\n- All Sensor metadata: `BMS/v1/PUB/Metadata/Sensor/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "LiquidTemperature",
            "LiquidDifferentialPressure",
            "LiquidFlow",
            "LiquidPressure",
            "AirTemperature",
            "AirDifferentialPressure",
            "AirRelativeHumidity",
            "AirFlow",
            "AirPressure",
            "LeakDetect",
            "Sound",
            "Available",
            "GenericPoint"
          ],
          "description": "Sensor point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "liquidTemperature": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidTemperature.payload.allOf[1]",
              {
                "type": "object",
                "properties": {
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "Sensor"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-282>"
                  }
                },
                "x-parser-schema-id": "SensorObjectTypeFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "SensorLiquidTemperatureMetadata"
          },
          "x-parser-unique-object-id": "liquidTemperature",
          "x-parser-message-name": "SensorLiquidTemperatureMsg"
        },
        "liquidDifferentialPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[1]",
              "$ref:$.channels.sensorMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "SensorLiquidDifferentialPressureMetadata"
          },
          "x-parser-unique-object-id": "liquidDifferentialPressure",
          "x-parser-message-name": "SensorLiquidDifferentialPressureMsg"
        },
        "liquidFlow": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidFlow.payload.allOf[1]",
              "$ref:$.channels.sensorMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "SensorLiquidFlowMetadata"
          },
          "x-parser-unique-object-id": "liquidFlow",
          "x-parser-message-name": "SensorLiquidFlowMsg"
        },
        "liquidPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidPressure.payload.allOf[1]",
              "$ref:$.channels.sensorMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "SensorLiquidPressureMetadata"
          },
          "x-parser-unique-object-id": "liquidPressure",
          "x-parser-message-name": "SensorLiquidPressureMsg"
        },
        "airTemperature": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airTemperature.payload.allOf[1]",
              "$ref:$.channels.sensorMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "SensorAirTemperatureMetadata"
          },
          "x-parser-unique-object-id": "airTemperature",
          "x-parser-message-name": "SensorAirTemperatureMsg"
        },
        "airDifferentialPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airDifferentialPressure.payload.allOf[1]",
              "$ref:$.channels.sensorMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "SensorAirDifferentialPressureMetadata"
          },
          "x-parser-unique-object-id": "airDifferentialPressure",
          "x-parser-message-name": "SensorAirDifferentialPressureMsg"
        },
        "airRelativeHumidity": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airRelativeHumidity.payload.allOf[1]",
              "$ref:$.channels.sensorMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "SensorAirRelativeHumidityMetadata"
          },
          "x-parser-unique-object-id": "airRelativeHumidity",
          "x-parser-message-name": "SensorAirRelativeHumidityMsg"
        },
        "airFlow": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airFlow.payload.allOf[1]",
              "$ref:$.channels.sensorMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "SensorAirFlowMetadata"
          },
          "x-parser-unique-object-id": "airFlow",
          "x-parser-message-name": "SensorAirFlowMsg"
        },
        "airPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airPressure.payload.allOf[1]",
              "$ref:$.channels.sensorMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "SensorAirPressureMetadata"
          },
          "x-parser-unique-object-id": "airPressure",
          "x-parser-message-name": "SensorAirPressureMsg"
        },
        "leakDetect": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.leakDetect.payload.allOf[1]",
              "$ref:$.channels.sensorMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "SensorLeakDetectMetadata"
          },
          "x-parser-unique-object-id": "leakDetect",
          "x-parser-message-name": "SensorLeakDetectMsg"
        },
        "available": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.available.payload.allOf[1]",
              "$ref:$.channels.sensorMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "SensorAvailableMetadata"
          },
          "x-parser-unique-object-id": "available",
          "x-parser-message-name": "SensorAvailableMsg"
        },
        "genericPoint": "$ref:$.channels.bessMetadata.messages.genericPoint",
        "sound": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              {
                "type": "object",
                "required": [
                  "engUnit"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "Sound"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-283>"
                  },
                  "engUnit": {
                    "type": "string",
                    "x-parser-schema-id": "<anonymous-schema-284>"
                  }
                },
                "x-parser-schema-id": "SoundFields"
              },
              "$ref:$.channels.sensorMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "SensorSoundMetadata"
          },
          "x-parser-unique-object-id": "sound",
          "x-parser-message-name": "SensorSoundMsg"
        }
      },
      "x-parser-unique-object-id": "sensorMetadata"
    },
    "tankValue": {
      "address": "BMS/v1/PUB/Value/Tank/{pointType}/{tagPath}",
      "description": "Real-time values published by the BMS for Tank points.\nSupports both liquid and air tanks.\n\n**MQTT wildcard examples**\n- All Tank values: `BMS/v1/PUB/Value/Tank/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "LiquidTemperature",
            "LiquidDifferentialPressure",
            "LiquidFlow",
            "LiquidPressure",
            "Status",
            "Available",
            "ValvePosition",
            "PumpSpeed",
            "FanSpeed",
            "DamperPosition",
            "AirTemperature",
            "AirDifferentialPressure",
            "AirRelativeHumidity",
            "AirFlow",
            "AirPressure",
            "LeakDetect",
            "GenericPoint"
          ],
          "description": "BMS-published Tank point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "valueMessage": "$ref:$.channels.rackBmsValue.messages.valueMessage"
      },
      "x-parser-unique-object-id": "tankValue"
    },
    "tankMetadata": {
      "address": "BMS/v1/PUB/Metadata/Tank/{pointType}/{tagPath}",
      "description": "BMS-published metadata for Tank points.\nIncludes integration-owned points which carries the `integration` field.\n\n**MQTT wildcard examples**\n- All Tank metadata: `BMS/v1/PUB/Metadata/Tank/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "LiquidTemperature",
            "LiquidDifferentialPressure",
            "LiquidFlow",
            "LiquidPressure",
            "Status",
            "Available",
            "ValvePosition",
            "PumpSpeed",
            "FanSpeed",
            "DamperPosition",
            "AirTemperature",
            "AirDifferentialPressure",
            "AirRelativeHumidity",
            "AirFlow",
            "AirPressure",
            "LeakDetect",
            "GenericPoint"
          ],
          "description": "Tank point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "liquidTemperature": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidTemperature.payload.allOf[1]",
              {
                "type": "object",
                "properties": {
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "Tank"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-289>"
                  }
                },
                "x-parser-schema-id": "TankObjectTypeFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "TankLiquidTemperatureMetadata"
          },
          "x-parser-unique-object-id": "liquidTemperature",
          "x-parser-message-name": "TankLiquidTemperatureMsg"
        },
        "liquidDifferentialPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[1]",
              "$ref:$.channels.tankMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "TankLiquidDifferentialPressureMetadata"
          },
          "x-parser-unique-object-id": "liquidDifferentialPressure",
          "x-parser-message-name": "TankLiquidDifferentialPressureMsg"
        },
        "liquidFlow": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidFlow.payload.allOf[1]",
              "$ref:$.channels.tankMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "TankLiquidFlowMetadata"
          },
          "x-parser-unique-object-id": "liquidFlow",
          "x-parser-message-name": "TankLiquidFlowMsg"
        },
        "liquidPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidPressure.payload.allOf[1]",
              "$ref:$.channels.tankMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "TankLiquidPressureMetadata"
          },
          "x-parser-unique-object-id": "liquidPressure",
          "x-parser-message-name": "TankLiquidPressureMsg"
        },
        "status": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[1]",
              "$ref:$.channels.tankMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "TankStatusMetadata"
          },
          "x-parser-unique-object-id": "status",
          "x-parser-message-name": "TankStatusMsg"
        },
        "available": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.available.payload.allOf[1]",
              "$ref:$.channels.tankMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "TankAvailableMetadata"
          },
          "x-parser-unique-object-id": "available",
          "x-parser-message-name": "TankAvailableMsg"
        },
        "valvePosition": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.valvePosition.payload.allOf[1]",
              "$ref:$.channels.tankMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "TankValvePositionMetadata"
          },
          "x-parser-unique-object-id": "valvePosition",
          "x-parser-message-name": "TankValvePositionMsg"
        },
        "pumpSpeed": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.pumpSpeed.payload.allOf[1]",
              "$ref:$.channels.tankMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "TankPumpSpeedMetadata"
          },
          "x-parser-unique-object-id": "pumpSpeed",
          "x-parser-message-name": "TankPumpSpeedMsg"
        },
        "fanSpeed": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.fanSpeed.payload.allOf[1]",
              "$ref:$.channels.tankMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "TankFanSpeedMetadata"
          },
          "x-parser-unique-object-id": "fanSpeed",
          "x-parser-message-name": "TankFanSpeedMsg"
        },
        "damperPosition": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.damperPosition.payload.allOf[1]",
              "$ref:$.channels.tankMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "TankDamperPositionMetadata"
          },
          "x-parser-unique-object-id": "damperPosition",
          "x-parser-message-name": "TankDamperPositionMsg"
        },
        "airTemperature": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airTemperature.payload.allOf[1]",
              "$ref:$.channels.tankMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "TankAirTemperatureMetadata"
          },
          "x-parser-unique-object-id": "airTemperature",
          "x-parser-message-name": "TankAirTemperatureMsg"
        },
        "airDifferentialPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airDifferentialPressure.payload.allOf[1]",
              "$ref:$.channels.tankMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "TankAirDifferentialPressureMetadata"
          },
          "x-parser-unique-object-id": "airDifferentialPressure",
          "x-parser-message-name": "TankAirDifferentialPressureMsg"
        },
        "airRelativeHumidity": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airRelativeHumidity.payload.allOf[1]",
              "$ref:$.channels.tankMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "TankAirRelativeHumidityMetadata"
          },
          "x-parser-unique-object-id": "airRelativeHumidity",
          "x-parser-message-name": "TankAirRelativeHumidityMsg"
        },
        "airFlow": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airFlow.payload.allOf[1]",
              "$ref:$.channels.tankMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "TankAirFlowMetadata"
          },
          "x-parser-unique-object-id": "airFlow",
          "x-parser-message-name": "TankAirFlowMsg"
        },
        "airPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airPressure.payload.allOf[1]",
              "$ref:$.channels.tankMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "TankAirPressureMetadata"
          },
          "x-parser-unique-object-id": "airPressure",
          "x-parser-message-name": "TankAirPressureMsg"
        },
        "leakDetect": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.leakDetect.payload.allOf[1]",
              "$ref:$.channels.tankMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "TankLeakDetectMetadata"
          },
          "x-parser-unique-object-id": "leakDetect",
          "x-parser-message-name": "TankLeakDetectMsg"
        },
        "genericPoint": "$ref:$.channels.bessMetadata.messages.genericPoint"
      },
      "x-parser-unique-object-id": "tankMetadata"
    },
    "genericObjectValue": {
      "address": "BMS/v1/PUB/Value/GenericObject/{pointType}/{tagPath}",
      "description": "Real-time values published by the BMS for GenericObject points.\nUse for any equipment type not covered by a named objectType.\n\n**MQTT wildcard examples**\n- All GenericObject values: `BMS/v1/PUB/Value/GenericObject/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "LiquidTemperature",
            "LiquidDifferentialPressure",
            "LiquidFlow",
            "LiquidPressure",
            "Status",
            "Available",
            "ValvePosition",
            "PumpSpeed",
            "FanSpeed",
            "DamperPosition",
            "AirTemperature",
            "AirDifferentialPressure",
            "AirRelativeHumidity",
            "AirFlow",
            "AirPressure",
            "LeakDetect",
            "Sound",
            "GenericPoint"
          ],
          "description": "BMS-published GenericObject point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "valueMessage": "$ref:$.channels.rackBmsValue.messages.valueMessage"
      },
      "x-parser-unique-object-id": "genericObjectValue"
    },
    "genericObjectMetadata": {
      "address": "BMS/v1/PUB/Metadata/GenericObject/{pointType}/{tagPath}",
      "description": "BMS-published metadata for GenericObject points.\nIncludes integration-owned points which carries the `integration` field.\n\n**MQTT wildcard examples**\n- All GenericObject metadata: `BMS/v1/PUB/Metadata/GenericObject/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "LiquidTemperature",
            "LiquidDifferentialPressure",
            "LiquidFlow",
            "LiquidPressure",
            "Status",
            "Available",
            "ValvePosition",
            "PumpSpeed",
            "FanSpeed",
            "DamperPosition",
            "AirTemperature",
            "AirDifferentialPressure",
            "AirRelativeHumidity",
            "AirFlow",
            "AirPressure",
            "LeakDetect",
            "LiquidTemperatureSpRequest",
            "Sound",
            "GenericPoint"
          ],
          "description": "GenericObject point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "liquidTemperature": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidTemperature.payload.allOf[1]",
              {
                "type": "object",
                "properties": {
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "GenericObject"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-294>"
                  }
                },
                "x-parser-schema-id": "GenericObjectObjectTypeFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "GenericObjectLiquidTemperatureMetadata"
          },
          "x-parser-unique-object-id": "liquidTemperature",
          "x-parser-message-name": "GenericObjectLiquidTemperatureMsg"
        },
        "liquidDifferentialPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[1]",
              "$ref:$.channels.genericObjectMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "GenericObjectLiquidDifferentialPressureMetadata"
          },
          "x-parser-unique-object-id": "liquidDifferentialPressure",
          "x-parser-message-name": "GenericObjectLiquidDifferentialPressureMsg"
        },
        "liquidFlow": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidFlow.payload.allOf[1]",
              "$ref:$.channels.genericObjectMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "GenericObjectLiquidFlowMetadata"
          },
          "x-parser-unique-object-id": "liquidFlow",
          "x-parser-message-name": "GenericObjectLiquidFlowMsg"
        },
        "liquidPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.liquidPressure.payload.allOf[1]",
              "$ref:$.channels.genericObjectMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "GenericObjectLiquidPressureMetadata"
          },
          "x-parser-unique-object-id": "liquidPressure",
          "x-parser-message-name": "GenericObjectLiquidPressureMsg"
        },
        "status": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[1]",
              "$ref:$.channels.genericObjectMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "GenericObjectStatusMetadata"
          },
          "x-parser-unique-object-id": "status",
          "x-parser-message-name": "GenericObjectStatusMsg"
        },
        "available": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.available.payload.allOf[1]",
              "$ref:$.channels.genericObjectMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "GenericObjectAvailableMetadata"
          },
          "x-parser-unique-object-id": "available",
          "x-parser-message-name": "GenericObjectAvailableMsg"
        },
        "valvePosition": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.valvePosition.payload.allOf[1]",
              "$ref:$.channels.genericObjectMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "GenericObjectValvePositionMetadata"
          },
          "x-parser-unique-object-id": "valvePosition",
          "x-parser-message-name": "GenericObjectValvePositionMsg"
        },
        "pumpSpeed": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.pumpSpeed.payload.allOf[1]",
              "$ref:$.channels.genericObjectMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "GenericObjectPumpSpeedMetadata"
          },
          "x-parser-unique-object-id": "pumpSpeed",
          "x-parser-message-name": "GenericObjectPumpSpeedMsg"
        },
        "fanSpeed": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.fanSpeed.payload.allOf[1]",
              "$ref:$.channels.genericObjectMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "GenericObjectFanSpeedMetadata"
          },
          "x-parser-unique-object-id": "fanSpeed",
          "x-parser-message-name": "GenericObjectFanSpeedMsg"
        },
        "damperPosition": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.damperPosition.payload.allOf[1]",
              "$ref:$.channels.genericObjectMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "GenericObjectDamperPositionMetadata"
          },
          "x-parser-unique-object-id": "damperPosition",
          "x-parser-message-name": "GenericObjectDamperPositionMsg"
        },
        "airTemperature": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airTemperature.payload.allOf[1]",
              "$ref:$.channels.genericObjectMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "GenericObjectAirTemperatureMetadata"
          },
          "x-parser-unique-object-id": "airTemperature",
          "x-parser-message-name": "GenericObjectAirTemperatureMsg"
        },
        "airDifferentialPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airDifferentialPressure.payload.allOf[1]",
              "$ref:$.channels.genericObjectMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "GenericObjectAirDifferentialPressureMetadata"
          },
          "x-parser-unique-object-id": "airDifferentialPressure",
          "x-parser-message-name": "GenericObjectAirDifferentialPressureMsg"
        },
        "airRelativeHumidity": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airRelativeHumidity.payload.allOf[1]",
              "$ref:$.channels.genericObjectMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "GenericObjectAirRelativeHumidityMetadata"
          },
          "x-parser-unique-object-id": "airRelativeHumidity",
          "x-parser-message-name": "GenericObjectAirRelativeHumidityMsg"
        },
        "airFlow": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airFlow.payload.allOf[1]",
              "$ref:$.channels.genericObjectMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "GenericObjectAirFlowMetadata"
          },
          "x-parser-unique-object-id": "airFlow",
          "x-parser-message-name": "GenericObjectAirFlowMsg"
        },
        "airPressure": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.airPressure.payload.allOf[1]",
              "$ref:$.channels.genericObjectMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "GenericObjectAirPressureMetadata"
          },
          "x-parser-unique-object-id": "airPressure",
          "x-parser-message-name": "GenericObjectAirPressureMsg"
        },
        "leakDetect": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.cduMetadata.messages.leakDetect.payload.allOf[1]",
              "$ref:$.channels.genericObjectMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "GenericObjectLeakDetectMetadata"
          },
          "x-parser-unique-object-id": "leakDetect",
          "x-parser-message-name": "GenericObjectLeakDetectMsg"
        },
        "liquidTemperatureSpRequest": {
          "payload": {
            "allOf": [
              "$ref:$.channels.cduMetadata.messages.liquidTemperatureSpRequest.payload.allOf[0]",
              {
                "type": "object",
                "description": "Setpoint request for a GenericObject. BMS publishes metadata; the integration publishes the value to the derived topic.\n",
                "required": [
                  "engUnit"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "LiquidTemperatureSpRequest"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-295>"
                  },
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "GenericObject"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-296>"
                  },
                  "engUnit": {
                    "type": "string",
                    "enum": [
                      "C"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-297>"
                  }
                },
                "x-parser-schema-id": "GenericObjectLiquidTemperatureSpRequestFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "GenericObjectLiquidTemperatureSpRequestMetadata"
          },
          "x-parser-unique-object-id": "liquidTemperatureSpRequest",
          "x-parser-message-name": "GenericObjectLiquidTemperatureSpRequestMsg"
        },
        "sound": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.sensorMetadata.messages.sound.payload.allOf[1]",
              "$ref:$.channels.genericObjectMetadata.messages.liquidTemperature.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "GenericObjectSoundMetadata"
          },
          "x-parser-unique-object-id": "sound",
          "x-parser-message-name": "GenericObjectSoundMsg"
        },
        "genericPoint": "$ref:$.channels.bessMetadata.messages.genericPoint"
      },
      "x-parser-unique-object-id": "genericObjectMetadata"
    },
    "genericObjectIntegrationValue": {
      "address": "BMS/v1/{integration}/Value/GenericObject/{pointType}/{tagPath}",
      "description": "Values published by integrations for GenericObject control points.\nSubscribe to `genericObjectMetadata` first and read `integration` for the exact topic.\n\n**MQTT wildcard examples**\n- All integration GenericObject values: `BMS/v1/+/Value/GenericObject/#`\n",
      "parameters": {
        "integration": {
          "description": "Integration identifier."
        },
        "pointType": {
          "enum": [
            "LiquidTemperatureSpRequest"
          ],
          "description": "Integration-published GenericObject point type."
        },
        "tagPath": {
          "description": "Must match the tagPath from the corresponding BMS metadata exactly."
        }
      },
      "messages": {
        "valueMessage": "$ref:$.channels.rackBmsValue.messages.valueMessage"
      },
      "x-parser-unique-object-id": "genericObjectIntegrationValue"
    },
    "systemBmsValue": {
      "address": "BMS/v1/PUB/Value/System/{pointType}/{tagPath}",
      "description": "BMS-published System values.\n\n**MQTT wildcard examples**\n- All System values: `BMS/v1/PUB/Value/System/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "HeartbeatTimestampBms",
            "HeartbeatEchoBms",
            "Status",
            "Available",
            "GenericPoint"
          ],
          "description": "BMS-published System point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "heartbeatEchoBms": {
          "payload": {
            "allOf": [
              "$ref:$.channels.rackMetadata.messages.rackLiquidSupplyTemperature.payload.allOf[0].allOf[0]",
              {
                "type": "object",
                "description": "BMS echoes back the timestamp received from a specific integration.",
                "required": [
                  "integration"
                ],
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "HeartbeatEchoBms"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-303>"
                  },
                  "integration": {
                    "type": "string",
                    "description": "Integration whose timestamp is being echoed.",
                    "x-parser-schema-id": "<anonymous-schema-304>"
                  }
                },
                "x-parser-schema-id": "HeartbeatEchoBmsFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "SystemHeartbeatEchoBmsMetadata"
          },
          "x-parser-unique-object-id": "heartbeatEchoBms",
          "x-parser-message-name": "SystemHeartbeatEchoBmsMsg"
        },
        "valueMessage": "$ref:$.channels.rackBmsValue.messages.valueMessage"
      },
      "x-parser-unique-object-id": "systemBmsValue"
    },
    "systemMetadata": {
      "address": "BMS/v1/PUB/Metadata/System/{pointType}/{tagPath}",
      "description": "BMS-published metadata for all System point types.\n\n**MQTT wildcard examples**\n- All System metadata: `BMS/v1/PUB/Metadata/System/#`\n",
      "parameters": {
        "pointType": {
          "enum": [
            "HeartbeatTimestampBms",
            "HeartbeatEchoBms",
            "HearbeatTimestampIntegration",
            "HeartbeatEchoIntegration",
            "Status",
            "Available",
            "GenericPoint"
          ],
          "description": "System point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "heartbeatTimestampBms": {
          "payload": {
            "allOf": [
              "$ref:$.channels.rackMetadata.messages.rackLiquidSupplyTemperature.payload.allOf[0].allOf[0]",
              {
                "type": "object",
                "description": "Required heartbeat. Written every 5 s. `scope` optionally identifies which MQTT topics this heartbeat covers.\n",
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "HeartbeatTimestampBms"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-307>"
                  },
                  "scope": {
                    "type": "string",
                    "description": "Identifies which MQTT topics this heartbeat covers.",
                    "x-parser-schema-id": "<anonymous-schema-308>"
                  }
                },
                "x-parser-schema-id": "HeartbeatTimestampBmsFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "SystemHeartbeatTimestampBmsMetadata"
          },
          "x-parser-unique-object-id": "heartbeatTimestampBms",
          "x-parser-message-name": "SystemHeartbeatTimestampBmsMsg"
        },
        "heartbeatEchoBms": "$ref:$.channels.systemBmsValue.messages.heartbeatEchoBms",
        "hearbeatTimestampIntegration": {
          "payload": {
            "allOf": [
              {
                "description": "Composed base for Integration-published System metadata messages. Combines MetadataBase with SystemIntegrationIdentifiers.\n",
                "allOf": [
                  "$ref:$.channels.rackMetadata.messages.rackLiquidSupplyTemperature.payload.allOf[0].allOf[0]",
                  {
                    "type": "object",
                    "description": "Required `integration` field for integration-published System metadata messages.\n",
                    "required": [
                      "integration"
                    ],
                    "properties": {
                      "integration": {
                        "type": "string",
                        "description": "Integration identifier.",
                        "x-parser-schema-id": "<anonymous-schema-309>"
                      }
                    },
                    "x-parser-schema-id": "SystemIntegrationIdentifiers"
                  }
                ],
                "x-parser-schema-id": "SystemIntegrationMetadataBase"
              },
              {
                "type": "object",
                "description": "Integration heartbeat. Written every 5 s.",
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "HearbeatTimestampIntegration"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-310>"
                  }
                },
                "x-parser-schema-id": "HearbeatTimestampIntegrationFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "SystemHearbeatTimestampIntegrationMetadata"
          },
          "x-parser-unique-object-id": "hearbeatTimestampIntegration",
          "x-parser-message-name": "SystemHearbeatTimestampIntegrationMsg"
        },
        "heartbeatEchoIntegration": {
          "payload": {
            "allOf": [
              "$ref:$.channels.systemMetadata.messages.hearbeatTimestampIntegration.payload.allOf[0]",
              {
                "type": "object",
                "description": "Integration echoes the BMS timestamp, once per BMS client/scope.",
                "properties": {
                  "pointType": {
                    "type": "string",
                    "enum": [
                      "HeartbeatEchoIntegration"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-311>"
                  },
                  "scope": {
                    "type": "string",
                    "description": "BMS scope being echoed (optional).",
                    "x-parser-schema-id": "<anonymous-schema-312>"
                  }
                },
                "x-parser-schema-id": "HeartbeatEchoIntegrationFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "SystemHeartbeatEchoIntegrationMetadata"
          },
          "x-parser-unique-object-id": "heartbeatEchoIntegration",
          "x-parser-message-name": "SystemHeartbeatEchoIntegrationMsg"
        },
        "status": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[1]",
              {
                "type": "object",
                "properties": {
                  "objectType": {
                    "type": "string",
                    "enum": [
                      "System"
                    ],
                    "x-parser-schema-id": "<anonymous-schema-313>"
                  }
                },
                "x-parser-schema-id": "SystemObjectTypeFields"
              }
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "SystemStatusMetadata"
          },
          "x-parser-unique-object-id": "status",
          "x-parser-message-name": "SystemStatusMsg"
        },
        "available": {
          "payload": {
            "allOf": [
              "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
              "$ref:$.channels.bessMetadata.messages.available.payload.allOf[1]",
              "$ref:$.channels.systemMetadata.messages.status.payload.allOf[2]"
            ],
            "unevaluatedProperties": false,
            "x-parser-schema-id": "SystemAvailableMetadata"
          },
          "x-parser-unique-object-id": "available",
          "x-parser-message-name": "SystemAvailableMsg"
        },
        "genericPoint": "$ref:$.channels.bessMetadata.messages.genericPoint"
      },
      "x-parser-unique-object-id": "systemMetadata"
    },
    "systemIntegrationValue": {
      "address": "BMS/v1/{integration}/Value/System/{pointType}/{tagPath}",
      "description": "Values published by integrations for Heartbeat points.\n\n**MQTT wildcard examples**\n- All integration heartbeat values: `BMS/v1/+/Value/System/#`\n",
      "parameters": {
        "integration": {
          "description": "Integration identifier."
        },
        "pointType": {
          "enum": [
            "HearbeatTimestampIntegration",
            "HeartbeatEchoIntegration"
          ],
          "description": "Integration-published System point type."
        },
        "tagPath": {
          "description": "Vendor-defined hierarchical tag path."
        }
      },
      "messages": {
        "valueMessage": "$ref:$.channels.rackBmsValue.messages.valueMessage"
      },
      "x-parser-unique-object-id": "systemIntegrationValue"
    }
  },
  "operations": {
    "receiveRackValue": {
      "action": "receive",
      "channel": "$ref:$.channels.rackBmsValue",
      "messages": [
        "$ref:$.channels.rackBmsValue.messages.valueMessage"
      ],
      "description": "Subscribe to real-time BMS-published values for all Rack points.",
      "x-parser-unique-object-id": "receiveRackValue"
    },
    "receiveRackMetadata": {
      "action": "receive",
      "channel": "$ref:$.channels.rackMetadata",
      "messages": [
        "$ref:$.channels.rackMetadata.messages.rackLiquidSupplyTemperature",
        "$ref:$.channels.rackMetadata.messages.rackLiquidReturnTemperature",
        "$ref:$.channels.rackMetadata.messages.rackLiquidFlow",
        "$ref:$.channels.rackMetadata.messages.rackLiquidDifferentialPressure",
        "$ref:$.channels.rackMetadata.messages.rackLiquidDifferentialPressureSp",
        "$ref:$.channels.rackMetadata.messages.rackControlValvePosition",
        "$ref:$.channels.rackMetadata.messages.rackPower",
        "$ref:$.channels.rackMetadata.messages.rackLeakDetect",
        "$ref:$.channels.rackMetadata.messages.rackLeakSensorFault",
        "$ref:$.channels.rackMetadata.messages.rackLeakDetectTray",
        "$ref:$.channels.rackMetadata.messages.rackLiquidIsolationStatus",
        "$ref:$.channels.rackMetadata.messages.rackElectricalIsolationStatus",
        "$ref:$.channels.rackMetadata.messages.rackLiquidIsolationRequest",
        "$ref:$.channels.rackMetadata.messages.rackElectricalIsolationRequest"
      ],
      "description": "Subscribe to BMS-published metadata for all Rack point types.",
      "x-parser-unique-object-id": "receiveRackMetadata"
    },
    "publishRackIntegrationValue": {
      "action": "send",
      "channel": "$ref:$.channels.rackIntegrationValue",
      "messages": [
        "$ref:$.channels.rackBmsValue.messages.valueMessage"
      ],
      "description": "Publish integration values for Rack control points.",
      "x-parser-unique-object-id": "publishRackIntegrationValue"
    },
    "receivePowerMeterValue": {
      "action": "receive",
      "channel": "$ref:$.channels.powerMeterValue",
      "messages": [
        "$ref:$.channels.rackBmsValue.messages.valueMessage"
      ],
      "description": "Subscribe to real-time BMS-published values for all PowerMeter points.",
      "x-parser-unique-object-id": "receivePowerMeterValue"
    },
    "receivePowerMeterMetadata": {
      "action": "receive",
      "channel": "$ref:$.channels.powerMeterMetadata",
      "messages": [
        "$ref:$.channels.powerMeterMetadata.messages.voltage",
        "$ref:$.channels.powerMeterMetadata.messages.powerFactor",
        "$ref:$.channels.powerMeterMetadata.messages.frequency",
        "$ref:$.channels.powerMeterMetadata.messages.apparentPower",
        "$ref:$.channels.powerMeterMetadata.messages.activePower",
        "$ref:$.channels.powerMeterMetadata.messages.current",
        "$ref:$.channels.powerMeterMetadata.messages.currentLimit",
        "$ref:$.channels.powerMeterMetadata.messages.phaseCurrent",
        "$ref:$.channels.powerMeterMetadata.messages.genericPoint"
      ],
      "description": "Subscribe to BMS-published metadata for all PowerMeter points.",
      "x-parser-unique-object-id": "receivePowerMeterMetadata"
    },
    "receiveBESSValue": {
      "action": "receive",
      "channel": "$ref:$.channels.bessValue",
      "messages": [
        "$ref:$.channels.rackBmsValue.messages.valueMessage"
      ],
      "description": "Subscribe to real-time BMS-published values for BESS points.",
      "x-parser-unique-object-id": "receiveBESSValue"
    },
    "receiveBESSMetadata": {
      "action": "receive",
      "channel": "$ref:$.channels.bessMetadata",
      "messages": [
        "$ref:$.channels.bessMetadata.messages.status",
        "$ref:$.channels.bessMetadata.messages.available",
        "$ref:$.channels.bessMetadata.messages.genericPoint"
      ],
      "description": "Subscribe to BMS-published metadata for BESS points.",
      "x-parser-unique-object-id": "receiveBESSMetadata"
    },
    "receiveUPSValue": {
      "action": "receive",
      "channel": "$ref:$.channels.upsValue",
      "messages": [
        "$ref:$.channels.rackBmsValue.messages.valueMessage"
      ],
      "description": "Subscribe to real-time BMS-published values for UPS points.",
      "x-parser-unique-object-id": "receiveUPSValue"
    },
    "receiveUPSMetadata": {
      "action": "receive",
      "channel": "$ref:$.channels.upsMetadata",
      "messages": [
        "$ref:$.channels.upsMetadata.messages.status",
        "$ref:$.channels.upsMetadata.messages.available",
        "$ref:$.channels.bessMetadata.messages.genericPoint"
      ],
      "description": "Subscribe to BMS-published metadata for UPS points.",
      "x-parser-unique-object-id": "receiveUPSMetadata"
    },
    "receiveATSValue": {
      "action": "receive",
      "channel": "$ref:$.channels.atsValue",
      "messages": [
        "$ref:$.channels.rackBmsValue.messages.valueMessage"
      ],
      "description": "Subscribe to real-time BMS-published values for ATS points.",
      "x-parser-unique-object-id": "receiveATSValue"
    },
    "receiveATSMetadata": {
      "action": "receive",
      "channel": "$ref:$.channels.atsMetadata",
      "messages": [
        "$ref:$.channels.atsMetadata.messages.status",
        "$ref:$.channels.atsMetadata.messages.available",
        "$ref:$.channels.bessMetadata.messages.genericPoint"
      ],
      "description": "Subscribe to BMS-published metadata for ATS points.",
      "x-parser-unique-object-id": "receiveATSMetadata"
    },
    "receiveGeneratorValue": {
      "action": "receive",
      "channel": "$ref:$.channels.generatorValue",
      "messages": [
        "$ref:$.channels.rackBmsValue.messages.valueMessage"
      ],
      "description": "Subscribe to real-time BMS-published values for Generator points.",
      "x-parser-unique-object-id": "receiveGeneratorValue"
    },
    "receiveGeneratorMetadata": {
      "action": "receive",
      "channel": "$ref:$.channels.generatorMetadata",
      "messages": [
        "$ref:$.channels.generatorMetadata.messages.status",
        "$ref:$.channels.generatorMetadata.messages.available",
        "$ref:$.channels.bessMetadata.messages.genericPoint"
      ],
      "description": "Subscribe to BMS-published metadata for Generator points.",
      "x-parser-unique-object-id": "receiveGeneratorMetadata"
    },
    "receiveShuntValue": {
      "action": "receive",
      "channel": "$ref:$.channels.shuntValue",
      "messages": [
        "$ref:$.channels.rackBmsValue.messages.valueMessage"
      ],
      "description": "Subscribe to real-time BMS-published values for Shunt points.",
      "x-parser-unique-object-id": "receiveShuntValue"
    },
    "receiveShuntMetadata": {
      "action": "receive",
      "channel": "$ref:$.channels.shuntMetadata",
      "messages": [
        "$ref:$.channels.shuntMetadata.messages.status",
        "$ref:$.channels.shuntMetadata.messages.available",
        "$ref:$.channels.bessMetadata.messages.genericPoint"
      ],
      "description": "Subscribe to BMS-published metadata for Shunt points.",
      "x-parser-unique-object-id": "receiveShuntMetadata"
    },
    "receiveBreakerValue": {
      "action": "receive",
      "channel": "$ref:$.channels.breakerValue",
      "messages": [
        "$ref:$.channels.rackBmsValue.messages.valueMessage"
      ],
      "description": "Subscribe to real-time BMS-published values for Breaker points.",
      "x-parser-unique-object-id": "receiveBreakerValue"
    },
    "receiveBreakerMetadata": {
      "action": "receive",
      "channel": "$ref:$.channels.breakerMetadata",
      "messages": [
        "$ref:$.channels.breakerMetadata.messages.status",
        "$ref:$.channels.breakerMetadata.messages.available",
        "$ref:$.channels.bessMetadata.messages.genericPoint"
      ],
      "description": "Subscribe to BMS-published metadata for Breaker points.",
      "x-parser-unique-object-id": "receiveBreakerMetadata"
    },
    "receiveCDUValue": {
      "action": "receive",
      "channel": "$ref:$.channels.cduValue",
      "messages": [
        "$ref:$.channels.rackBmsValue.messages.valueMessage"
      ],
      "description": "Subscribe to real-time BMS-published values for CDU points.",
      "x-parser-unique-object-id": "receiveCDUValue"
    },
    "receiveCDUMetadata": {
      "action": "receive",
      "channel": "$ref:$.channels.cduMetadata",
      "messages": [
        "$ref:$.channels.cduMetadata.messages.liquidTemperature",
        "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure",
        "$ref:$.channels.cduMetadata.messages.liquidFlow",
        "$ref:$.channels.cduMetadata.messages.liquidPressure",
        "$ref:$.channels.cduMetadata.messages.status",
        "$ref:$.channels.cduMetadata.messages.available",
        "$ref:$.channels.cduMetadata.messages.valvePosition",
        "$ref:$.channels.cduMetadata.messages.pumpSpeed",
        "$ref:$.channels.cduMetadata.messages.fanSpeed",
        "$ref:$.channels.cduMetadata.messages.damperPosition",
        "$ref:$.channels.cduMetadata.messages.airTemperature",
        "$ref:$.channels.cduMetadata.messages.airDifferentialPressure",
        "$ref:$.channels.cduMetadata.messages.airRelativeHumidity",
        "$ref:$.channels.cduMetadata.messages.airFlow",
        "$ref:$.channels.cduMetadata.messages.airPressure",
        "$ref:$.channels.cduMetadata.messages.leakDetect",
        "$ref:$.channels.cduMetadata.messages.liquidTemperatureSpRequest",
        "$ref:$.channels.bessMetadata.messages.genericPoint"
      ],
      "description": "Subscribe to BMS-published metadata for CDU points.",
      "x-parser-unique-object-id": "receiveCDUMetadata"
    },
    "publishCDUIntegrationValue": {
      "action": "send",
      "channel": "$ref:$.channels.cduIntegrationValue",
      "messages": [
        "$ref:$.channels.rackBmsValue.messages.valueMessage"
      ],
      "description": "Publish integration values for CDU control points.",
      "x-parser-unique-object-id": "publishCDUIntegrationValue"
    },
    "receiveCoolingTowerValue": {
      "action": "receive",
      "channel": "$ref:$.channels.coolingTowerValue",
      "messages": [
        "$ref:$.channels.rackBmsValue.messages.valueMessage"
      ],
      "description": "Subscribe to real-time BMS-published values for CoolingTower points.",
      "x-parser-unique-object-id": "receiveCoolingTowerValue"
    },
    "receiveCoolingTowerMetadata": {
      "action": "receive",
      "channel": "$ref:$.channels.coolingTowerMetadata",
      "messages": [
        "$ref:$.channels.coolingTowerMetadata.messages.liquidTemperature",
        "$ref:$.channels.coolingTowerMetadata.messages.liquidDifferentialPressure",
        "$ref:$.channels.coolingTowerMetadata.messages.liquidFlow",
        "$ref:$.channels.coolingTowerMetadata.messages.liquidPressure",
        "$ref:$.channels.coolingTowerMetadata.messages.status",
        "$ref:$.channels.coolingTowerMetadata.messages.available",
        "$ref:$.channels.coolingTowerMetadata.messages.valvePosition",
        "$ref:$.channels.coolingTowerMetadata.messages.pumpSpeed",
        "$ref:$.channels.coolingTowerMetadata.messages.fanSpeed",
        "$ref:$.channels.coolingTowerMetadata.messages.damperPosition",
        "$ref:$.channels.coolingTowerMetadata.messages.airTemperature",
        "$ref:$.channels.coolingTowerMetadata.messages.airDifferentialPressure",
        "$ref:$.channels.coolingTowerMetadata.messages.airRelativeHumidity",
        "$ref:$.channels.coolingTowerMetadata.messages.airFlow",
        "$ref:$.channels.coolingTowerMetadata.messages.airPressure",
        "$ref:$.channels.coolingTowerMetadata.messages.leakDetect",
        "$ref:$.channels.bessMetadata.messages.genericPoint"
      ],
      "description": "Subscribe to BMS-published metadata for CoolingTower points.",
      "x-parser-unique-object-id": "receiveCoolingTowerMetadata"
    },
    "receiveHXValue": {
      "action": "receive",
      "channel": "$ref:$.channels.hxValue",
      "messages": [
        "$ref:$.channels.rackBmsValue.messages.valueMessage"
      ],
      "description": "Subscribe to real-time BMS-published values for HX points.",
      "x-parser-unique-object-id": "receiveHXValue"
    },
    "receiveHXMetadata": {
      "action": "receive",
      "channel": "$ref:$.channels.hxMetadata",
      "messages": [
        "$ref:$.channels.hxMetadata.messages.liquidTemperature",
        "$ref:$.channels.hxMetadata.messages.liquidDifferentialPressure",
        "$ref:$.channels.hxMetadata.messages.liquidFlow",
        "$ref:$.channels.hxMetadata.messages.liquidPressure",
        "$ref:$.channels.hxMetadata.messages.status",
        "$ref:$.channels.hxMetadata.messages.available",
        "$ref:$.channels.hxMetadata.messages.valvePosition",
        "$ref:$.channels.hxMetadata.messages.pumpSpeed",
        "$ref:$.channels.hxMetadata.messages.fanSpeed",
        "$ref:$.channels.hxMetadata.messages.damperPosition",
        "$ref:$.channels.hxMetadata.messages.airTemperature",
        "$ref:$.channels.hxMetadata.messages.airDifferentialPressure",
        "$ref:$.channels.hxMetadata.messages.airRelativeHumidity",
        "$ref:$.channels.hxMetadata.messages.airFlow",
        "$ref:$.channels.hxMetadata.messages.airPressure",
        "$ref:$.channels.hxMetadata.messages.leakDetect",
        "$ref:$.channels.bessMetadata.messages.genericPoint"
      ],
      "description": "Subscribe to BMS-published metadata for HX points.",
      "x-parser-unique-object-id": "receiveHXMetadata"
    },
    "receiveCRAHValue": {
      "action": "receive",
      "channel": "$ref:$.channels.crahValue",
      "messages": [
        "$ref:$.channels.rackBmsValue.messages.valueMessage"
      ],
      "description": "Subscribe to real-time BMS-published values for CRAH points.",
      "x-parser-unique-object-id": "receiveCRAHValue"
    },
    "receiveCRAHMetadata": {
      "action": "receive",
      "channel": "$ref:$.channels.crahMetadata",
      "messages": [
        "$ref:$.channels.crahMetadata.messages.liquidTemperature",
        "$ref:$.channels.crahMetadata.messages.liquidDifferentialPressure",
        "$ref:$.channels.crahMetadata.messages.liquidFlow",
        "$ref:$.channels.crahMetadata.messages.liquidPressure",
        "$ref:$.channels.crahMetadata.messages.status",
        "$ref:$.channels.crahMetadata.messages.available",
        "$ref:$.channels.crahMetadata.messages.valvePosition",
        "$ref:$.channels.crahMetadata.messages.pumpSpeed",
        "$ref:$.channels.crahMetadata.messages.fanSpeed",
        "$ref:$.channels.crahMetadata.messages.damperPosition",
        "$ref:$.channels.crahMetadata.messages.airTemperature",
        "$ref:$.channels.crahMetadata.messages.airDifferentialPressure",
        "$ref:$.channels.crahMetadata.messages.airRelativeHumidity",
        "$ref:$.channels.crahMetadata.messages.airFlow",
        "$ref:$.channels.crahMetadata.messages.airPressure",
        "$ref:$.channels.crahMetadata.messages.leakDetect",
        "$ref:$.channels.bessMetadata.messages.genericPoint"
      ],
      "description": "Subscribe to BMS-published metadata for CRAH points.",
      "x-parser-unique-object-id": "receiveCRAHMetadata"
    },
    "receiveCRACValue": {
      "action": "receive",
      "channel": "$ref:$.channels.cracValue",
      "messages": [
        "$ref:$.channels.rackBmsValue.messages.valueMessage"
      ],
      "description": "Subscribe to real-time BMS-published values for CRAC points.",
      "x-parser-unique-object-id": "receiveCRACValue"
    },
    "receiveCRACMetadata": {
      "action": "receive",
      "channel": "$ref:$.channels.cracMetadata",
      "messages": [
        "$ref:$.channels.cracMetadata.messages.liquidTemperature",
        "$ref:$.channels.cracMetadata.messages.liquidDifferentialPressure",
        "$ref:$.channels.cracMetadata.messages.liquidFlow",
        "$ref:$.channels.cracMetadata.messages.liquidPressure",
        "$ref:$.channels.cracMetadata.messages.status",
        "$ref:$.channels.cracMetadata.messages.available",
        "$ref:$.channels.cracMetadata.messages.valvePosition",
        "$ref:$.channels.cracMetadata.messages.pumpSpeed",
        "$ref:$.channels.cracMetadata.messages.fanSpeed",
        "$ref:$.channels.cracMetadata.messages.damperPosition",
        "$ref:$.channels.cracMetadata.messages.airTemperature",
        "$ref:$.channels.cracMetadata.messages.airDifferentialPressure",
        "$ref:$.channels.cracMetadata.messages.airRelativeHumidity",
        "$ref:$.channels.cracMetadata.messages.airFlow",
        "$ref:$.channels.cracMetadata.messages.airPressure",
        "$ref:$.channels.cracMetadata.messages.leakDetect",
        "$ref:$.channels.bessMetadata.messages.genericPoint"
      ],
      "description": "Subscribe to BMS-published metadata for CRAC points.",
      "x-parser-unique-object-id": "receiveCRACMetadata"
    },
    "receiveAHUValue": {
      "action": "receive",
      "channel": "$ref:$.channels.ahuValue",
      "messages": [
        "$ref:$.channels.rackBmsValue.messages.valueMessage"
      ],
      "description": "Subscribe to real-time BMS-published values for AHU points.",
      "x-parser-unique-object-id": "receiveAHUValue"
    },
    "receiveAHUMetadata": {
      "action": "receive",
      "channel": "$ref:$.channels.ahuMetadata",
      "messages": [
        "$ref:$.channels.ahuMetadata.messages.liquidTemperature",
        "$ref:$.channels.ahuMetadata.messages.liquidDifferentialPressure",
        "$ref:$.channels.ahuMetadata.messages.liquidFlow",
        "$ref:$.channels.ahuMetadata.messages.liquidPressure",
        "$ref:$.channels.ahuMetadata.messages.status",
        "$ref:$.channels.ahuMetadata.messages.available",
        "$ref:$.channels.ahuMetadata.messages.valvePosition",
        "$ref:$.channels.ahuMetadata.messages.pumpSpeed",
        "$ref:$.channels.ahuMetadata.messages.fanSpeed",
        "$ref:$.channels.ahuMetadata.messages.damperPosition",
        "$ref:$.channels.ahuMetadata.messages.airTemperature",
        "$ref:$.channels.ahuMetadata.messages.airDifferentialPressure",
        "$ref:$.channels.ahuMetadata.messages.airRelativeHumidity",
        "$ref:$.channels.ahuMetadata.messages.airFlow",
        "$ref:$.channels.ahuMetadata.messages.airPressure",
        "$ref:$.channels.ahuMetadata.messages.leakDetect",
        "$ref:$.channels.bessMetadata.messages.genericPoint"
      ],
      "description": "Subscribe to BMS-published metadata for AHU points.",
      "x-parser-unique-object-id": "receiveAHUMetadata"
    },
    "receiveChillerValue": {
      "action": "receive",
      "channel": "$ref:$.channels.chillerValue",
      "messages": [
        "$ref:$.channels.rackBmsValue.messages.valueMessage"
      ],
      "description": "Subscribe to real-time BMS-published values for Chiller points.",
      "x-parser-unique-object-id": "receiveChillerValue"
    },
    "receiveChillerMetadata": {
      "action": "receive",
      "channel": "$ref:$.channels.chillerMetadata",
      "messages": [
        "$ref:$.channels.chillerMetadata.messages.liquidTemperature",
        "$ref:$.channels.chillerMetadata.messages.liquidDifferentialPressure",
        "$ref:$.channels.chillerMetadata.messages.liquidFlow",
        "$ref:$.channels.chillerMetadata.messages.liquidPressure",
        "$ref:$.channels.chillerMetadata.messages.status",
        "$ref:$.channels.chillerMetadata.messages.available",
        "$ref:$.channels.chillerMetadata.messages.valvePosition",
        "$ref:$.channels.chillerMetadata.messages.pumpSpeed",
        "$ref:$.channels.chillerMetadata.messages.fanSpeed",
        "$ref:$.channels.chillerMetadata.messages.damperPosition",
        "$ref:$.channels.chillerMetadata.messages.airTemperature",
        "$ref:$.channels.chillerMetadata.messages.airDifferentialPressure",
        "$ref:$.channels.chillerMetadata.messages.airRelativeHumidity",
        "$ref:$.channels.chillerMetadata.messages.airFlow",
        "$ref:$.channels.chillerMetadata.messages.airPressure",
        "$ref:$.channels.chillerMetadata.messages.leakDetect",
        "$ref:$.channels.bessMetadata.messages.genericPoint"
      ],
      "description": "Subscribe to BMS-published metadata for Chiller points.",
      "x-parser-unique-object-id": "receiveChillerMetadata"
    },
    "receiveValveValue": {
      "action": "receive",
      "channel": "$ref:$.channels.valveValue",
      "messages": [
        "$ref:$.channels.rackBmsValue.messages.valueMessage"
      ],
      "description": "Subscribe to real-time BMS-published values for Valve points.",
      "x-parser-unique-object-id": "receiveValveValue"
    },
    "receiveValveMetadata": {
      "action": "receive",
      "channel": "$ref:$.channels.valveMetadata",
      "messages": [
        "$ref:$.channels.valveMetadata.messages.valvePosition",
        "$ref:$.channels.valveMetadata.messages.available",
        "$ref:$.channels.bessMetadata.messages.genericPoint"
      ],
      "description": "Subscribe to BMS-published metadata for Valve points.",
      "x-parser-unique-object-id": "receiveValveMetadata"
    },
    "receivePumpValue": {
      "action": "receive",
      "channel": "$ref:$.channels.pumpValue",
      "messages": [
        "$ref:$.channels.rackBmsValue.messages.valueMessage"
      ],
      "description": "Subscribe to real-time BMS-published values for Pump points.",
      "x-parser-unique-object-id": "receivePumpValue"
    },
    "receivePumpMetadata": {
      "action": "receive",
      "channel": "$ref:$.channels.pumpMetadata",
      "messages": [
        "$ref:$.channels.pumpMetadata.messages.pumpSpeed",
        "$ref:$.channels.pumpMetadata.messages.available",
        "$ref:$.channels.bessMetadata.messages.genericPoint"
      ],
      "description": "Subscribe to BMS-published metadata for Pump points.",
      "x-parser-unique-object-id": "receivePumpMetadata"
    },
    "receiveFanValue": {
      "action": "receive",
      "channel": "$ref:$.channels.fanValue",
      "messages": [
        "$ref:$.channels.rackBmsValue.messages.valueMessage"
      ],
      "description": "Subscribe to real-time BMS-published values for Fan points.",
      "x-parser-unique-object-id": "receiveFanValue"
    },
    "receiveFanMetadata": {
      "action": "receive",
      "channel": "$ref:$.channels.fanMetadata",
      "messages": [
        "$ref:$.channels.fanMetadata.messages.fanSpeed",
        "$ref:$.channels.fanMetadata.messages.available",
        "$ref:$.channels.bessMetadata.messages.genericPoint"
      ],
      "description": "Subscribe to BMS-published metadata for Fan points.",
      "x-parser-unique-object-id": "receiveFanMetadata"
    },
    "receiveDamperValue": {
      "action": "receive",
      "channel": "$ref:$.channels.damperValue",
      "messages": [
        "$ref:$.channels.rackBmsValue.messages.valueMessage"
      ],
      "description": "Subscribe to real-time BMS-published values for Damper points.",
      "x-parser-unique-object-id": "receiveDamperValue"
    },
    "receiveDamperMetadata": {
      "action": "receive",
      "channel": "$ref:$.channels.damperMetadata",
      "messages": [
        "$ref:$.channels.damperMetadata.messages.damperPosition",
        "$ref:$.channels.damperMetadata.messages.available",
        "$ref:$.channels.bessMetadata.messages.genericPoint"
      ],
      "description": "Subscribe to BMS-published metadata for Damper points.",
      "x-parser-unique-object-id": "receiveDamperMetadata"
    },
    "receiveSensorValue": {
      "action": "receive",
      "channel": "$ref:$.channels.sensorValue",
      "messages": [
        "$ref:$.channels.rackBmsValue.messages.valueMessage"
      ],
      "description": "Subscribe to real-time BMS-published values for Sensor points.",
      "x-parser-unique-object-id": "receiveSensorValue"
    },
    "receiveSensorMetadata": {
      "action": "receive",
      "channel": "$ref:$.channels.sensorMetadata",
      "messages": [
        "$ref:$.channels.sensorMetadata.messages.liquidTemperature",
        "$ref:$.channels.sensorMetadata.messages.liquidDifferentialPressure",
        "$ref:$.channels.sensorMetadata.messages.liquidFlow",
        "$ref:$.channels.sensorMetadata.messages.liquidPressure",
        "$ref:$.channels.sensorMetadata.messages.airTemperature",
        "$ref:$.channels.sensorMetadata.messages.airDifferentialPressure",
        "$ref:$.channels.sensorMetadata.messages.airRelativeHumidity",
        "$ref:$.channels.sensorMetadata.messages.airFlow",
        "$ref:$.channels.sensorMetadata.messages.airPressure",
        "$ref:$.channels.sensorMetadata.messages.leakDetect",
        "$ref:$.channels.sensorMetadata.messages.available",
        "$ref:$.channels.sensorMetadata.messages.sound",
        "$ref:$.channels.bessMetadata.messages.genericPoint"
      ],
      "description": "Subscribe to BMS-published metadata for Sensor points.",
      "x-parser-unique-object-id": "receiveSensorMetadata"
    },
    "receiveTankValue": {
      "action": "receive",
      "channel": "$ref:$.channels.tankValue",
      "messages": [
        "$ref:$.channels.rackBmsValue.messages.valueMessage"
      ],
      "description": "Subscribe to real-time BMS-published values for Tank points.",
      "x-parser-unique-object-id": "receiveTankValue"
    },
    "receiveTankMetadata": {
      "action": "receive",
      "channel": "$ref:$.channels.tankMetadata",
      "messages": [
        "$ref:$.channels.tankMetadata.messages.liquidTemperature",
        "$ref:$.channels.tankMetadata.messages.liquidDifferentialPressure",
        "$ref:$.channels.tankMetadata.messages.liquidFlow",
        "$ref:$.channels.tankMetadata.messages.liquidPressure",
        "$ref:$.channels.tankMetadata.messages.status",
        "$ref:$.channels.tankMetadata.messages.available",
        "$ref:$.channels.tankMetadata.messages.valvePosition",
        "$ref:$.channels.tankMetadata.messages.pumpSpeed",
        "$ref:$.channels.tankMetadata.messages.fanSpeed",
        "$ref:$.channels.tankMetadata.messages.damperPosition",
        "$ref:$.channels.tankMetadata.messages.airTemperature",
        "$ref:$.channels.tankMetadata.messages.airDifferentialPressure",
        "$ref:$.channels.tankMetadata.messages.airRelativeHumidity",
        "$ref:$.channels.tankMetadata.messages.airFlow",
        "$ref:$.channels.tankMetadata.messages.airPressure",
        "$ref:$.channels.tankMetadata.messages.leakDetect",
        "$ref:$.channels.bessMetadata.messages.genericPoint"
      ],
      "description": "Subscribe to BMS-published metadata for Tank points.",
      "x-parser-unique-object-id": "receiveTankMetadata"
    },
    "receiveGenericObjectValue": {
      "action": "receive",
      "channel": "$ref:$.channels.genericObjectValue",
      "messages": [
        "$ref:$.channels.rackBmsValue.messages.valueMessage"
      ],
      "description": "Subscribe to real-time BMS-published values for GenericObject points.",
      "x-parser-unique-object-id": "receiveGenericObjectValue"
    },
    "receiveGenericObjectMetadata": {
      "action": "receive",
      "channel": "$ref:$.channels.genericObjectMetadata",
      "messages": [
        "$ref:$.channels.genericObjectMetadata.messages.liquidTemperature",
        "$ref:$.channels.genericObjectMetadata.messages.liquidDifferentialPressure",
        "$ref:$.channels.genericObjectMetadata.messages.liquidFlow",
        "$ref:$.channels.genericObjectMetadata.messages.liquidPressure",
        "$ref:$.channels.genericObjectMetadata.messages.status",
        "$ref:$.channels.genericObjectMetadata.messages.available",
        "$ref:$.channels.genericObjectMetadata.messages.valvePosition",
        "$ref:$.channels.genericObjectMetadata.messages.pumpSpeed",
        "$ref:$.channels.genericObjectMetadata.messages.fanSpeed",
        "$ref:$.channels.genericObjectMetadata.messages.damperPosition",
        "$ref:$.channels.genericObjectMetadata.messages.airTemperature",
        "$ref:$.channels.genericObjectMetadata.messages.airDifferentialPressure",
        "$ref:$.channels.genericObjectMetadata.messages.airRelativeHumidity",
        "$ref:$.channels.genericObjectMetadata.messages.airFlow",
        "$ref:$.channels.genericObjectMetadata.messages.airPressure",
        "$ref:$.channels.genericObjectMetadata.messages.leakDetect",
        "$ref:$.channels.genericObjectMetadata.messages.liquidTemperatureSpRequest",
        "$ref:$.channels.genericObjectMetadata.messages.sound",
        "$ref:$.channels.bessMetadata.messages.genericPoint"
      ],
      "description": "Subscribe to BMS-published metadata for GenericObject points.",
      "x-parser-unique-object-id": "receiveGenericObjectMetadata"
    },
    "publishGenericObjectIntegrationValue": {
      "action": "send",
      "channel": "$ref:$.channels.genericObjectIntegrationValue",
      "messages": [
        "$ref:$.channels.rackBmsValue.messages.valueMessage"
      ],
      "description": "Publish integration values for GenericObject control points.",
      "x-parser-unique-object-id": "publishGenericObjectIntegrationValue"
    },
    "receiveSystemBmsValue": {
      "action": "receive",
      "channel": "$ref:$.channels.systemBmsValue",
      "messages": [
        "$ref:$.channels.rackBmsValue.messages.valueMessage"
      ],
      "description": "Subscribe to BMS System values (heartbeat timestamps, status, etc.).",
      "x-parser-unique-object-id": "receiveSystemBmsValue"
    },
    "receiveSystemMetadata": {
      "action": "receive",
      "channel": "$ref:$.channels.systemMetadata",
      "messages": [
        "$ref:$.channels.systemMetadata.messages.heartbeatTimestampBms",
        "$ref:$.channels.systemBmsValue.messages.heartbeatEchoBms",
        "$ref:$.channels.systemMetadata.messages.hearbeatTimestampIntegration",
        "$ref:$.channels.systemMetadata.messages.heartbeatEchoIntegration"
      ],
      "description": "Subscribe to System metadata for all System point types.",
      "x-parser-unique-object-id": "receiveSystemMetadata"
    },
    "publishSystemIntegrationValue": {
      "action": "send",
      "channel": "$ref:$.channels.systemIntegrationValue",
      "messages": [
        "$ref:$.channels.rackBmsValue.messages.valueMessage"
      ],
      "description": "Publish integration System values to topics derived from BMS metadata.",
      "x-parser-unique-object-id": "publishSystemIntegrationValue"
    }
  },
  "components": {
    "messages": {
      "ValueMessage": "$ref:$.channels.rackBmsValue.messages.valueMessage",
      "RackLiquidSupplyTemperatureMsg": "$ref:$.channels.rackMetadata.messages.rackLiquidSupplyTemperature",
      "RackLiquidReturnTemperatureMsg": "$ref:$.channels.rackMetadata.messages.rackLiquidReturnTemperature",
      "RackLiquidFlowMsg": "$ref:$.channels.rackMetadata.messages.rackLiquidFlow",
      "RackLiquidDifferentialPressureMsg": "$ref:$.channels.rackMetadata.messages.rackLiquidDifferentialPressure",
      "RackControlValvePositionMsg": "$ref:$.channels.rackMetadata.messages.rackControlValvePosition",
      "RackPowerMsg": "$ref:$.channels.rackMetadata.messages.rackPower",
      "RackLeakDetectMsg": "$ref:$.channels.rackMetadata.messages.rackLeakDetect",
      "RackLeakSensorFaultMsg": "$ref:$.channels.rackMetadata.messages.rackLeakSensorFault",
      "RackLiquidIsolationStatusMsg": "$ref:$.channels.rackMetadata.messages.rackLiquidIsolationStatus",
      "RackElectricalIsolationStatusMsg": "$ref:$.channels.rackMetadata.messages.rackElectricalIsolationStatus",
      "RackLeakDetectTrayMsg": "$ref:$.channels.rackMetadata.messages.rackLeakDetectTray",
      "RackLiquidIsolationRequestMsg": "$ref:$.channels.rackMetadata.messages.rackLiquidIsolationRequest",
      "RackElectricalIsolationRequestMsg": "$ref:$.channels.rackMetadata.messages.rackElectricalIsolationRequest",
      "PowerMeterVoltageMsg": "$ref:$.channels.powerMeterMetadata.messages.voltage",
      "PowerMeterPowerFactorMsg": "$ref:$.channels.powerMeterMetadata.messages.powerFactor",
      "PowerMeterFrequencyMsg": "$ref:$.channels.powerMeterMetadata.messages.frequency",
      "PowerMeterApparentPowerMsg": "$ref:$.channels.powerMeterMetadata.messages.apparentPower",
      "PowerMeterActivePowerMsg": "$ref:$.channels.powerMeterMetadata.messages.activePower",
      "PowerMeterCurrentMsg": "$ref:$.channels.powerMeterMetadata.messages.current",
      "PowerMeterCurrentLimitMsg": "$ref:$.channels.powerMeterMetadata.messages.currentLimit",
      "PowerMeterPhaseCurrentMsg": "$ref:$.channels.powerMeterMetadata.messages.phaseCurrent",
      "LiquidTemperatureMsg": {
        "payload": {
          "allOf": [
            "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
            "$ref:$.channels.cduMetadata.messages.liquidTemperature.payload.allOf[1]"
          ],
          "unevaluatedProperties": false,
          "x-parser-schema-id": "LiquidTemperatureMetadata"
        },
        "x-parser-unique-object-id": "LiquidTemperatureMsg",
        "x-parser-message-name": "LiquidTemperatureMsg"
      },
      "LiquidDifferentialPressureMsg": {
        "payload": {
          "allOf": [
            "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
            "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[1]"
          ],
          "unevaluatedProperties": false,
          "x-parser-schema-id": "LiquidDifferentialPressureMetadata"
        },
        "x-parser-unique-object-id": "LiquidDifferentialPressureMsg",
        "x-parser-message-name": "LiquidDifferentialPressureMsg"
      },
      "LiquidFlowMsg": {
        "payload": {
          "allOf": [
            "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
            "$ref:$.channels.cduMetadata.messages.liquidFlow.payload.allOf[1]"
          ],
          "unevaluatedProperties": false,
          "x-parser-schema-id": "LiquidFlowMetadata"
        },
        "x-parser-unique-object-id": "LiquidFlowMsg",
        "x-parser-message-name": "LiquidFlowMsg"
      },
      "LiquidPressureMsg": {
        "payload": {
          "allOf": [
            "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
            "$ref:$.channels.cduMetadata.messages.liquidPressure.payload.allOf[1]"
          ],
          "unevaluatedProperties": false,
          "x-parser-schema-id": "LiquidPressureMetadata"
        },
        "x-parser-unique-object-id": "LiquidPressureMsg",
        "x-parser-message-name": "LiquidPressureMsg"
      },
      "StatusMsg": {
        "payload": {
          "allOf": [
            "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
            "$ref:$.channels.bessMetadata.messages.status.payload.allOf[1]"
          ],
          "unevaluatedProperties": false,
          "x-parser-schema-id": "StatusMetadata"
        },
        "x-parser-unique-object-id": "StatusMsg",
        "x-parser-message-name": "StatusMsg"
      },
      "AvailableMsg": {
        "payload": {
          "allOf": [
            "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
            "$ref:$.channels.bessMetadata.messages.available.payload.allOf[1]"
          ],
          "unevaluatedProperties": false,
          "x-parser-schema-id": "AvailableMetadata"
        },
        "x-parser-unique-object-id": "AvailableMsg",
        "x-parser-message-name": "AvailableMsg"
      },
      "ValvePositionMsg": {
        "payload": {
          "allOf": [
            "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
            "$ref:$.channels.cduMetadata.messages.valvePosition.payload.allOf[1]"
          ],
          "unevaluatedProperties": false,
          "x-parser-schema-id": "ValvePositionMetadata"
        },
        "x-parser-unique-object-id": "ValvePositionMsg",
        "x-parser-message-name": "ValvePositionMsg"
      },
      "PumpSpeedMsg": {
        "payload": {
          "allOf": [
            "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
            "$ref:$.channels.cduMetadata.messages.pumpSpeed.payload.allOf[1]"
          ],
          "unevaluatedProperties": false,
          "x-parser-schema-id": "PumpSpeedMetadata"
        },
        "x-parser-unique-object-id": "PumpSpeedMsg",
        "x-parser-message-name": "PumpSpeedMsg"
      },
      "FanSpeedMsg": {
        "payload": {
          "allOf": [
            "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
            "$ref:$.channels.cduMetadata.messages.fanSpeed.payload.allOf[1]"
          ],
          "unevaluatedProperties": false,
          "x-parser-schema-id": "FanSpeedMetadata"
        },
        "x-parser-unique-object-id": "FanSpeedMsg",
        "x-parser-message-name": "FanSpeedMsg"
      },
      "DamperPositionMsg": {
        "payload": {
          "allOf": [
            "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
            "$ref:$.channels.cduMetadata.messages.damperPosition.payload.allOf[1]"
          ],
          "unevaluatedProperties": false,
          "x-parser-schema-id": "DamperPositionMetadata"
        },
        "x-parser-unique-object-id": "DamperPositionMsg",
        "x-parser-message-name": "DamperPositionMsg"
      },
      "AirTemperatureMsg": {
        "payload": {
          "allOf": [
            "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
            "$ref:$.channels.cduMetadata.messages.airTemperature.payload.allOf[1]"
          ],
          "unevaluatedProperties": false,
          "x-parser-schema-id": "AirTemperatureMetadata"
        },
        "x-parser-unique-object-id": "AirTemperatureMsg",
        "x-parser-message-name": "AirTemperatureMsg"
      },
      "AirDifferentialPressureMsg": {
        "payload": {
          "allOf": [
            "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
            "$ref:$.channels.cduMetadata.messages.airDifferentialPressure.payload.allOf[1]"
          ],
          "unevaluatedProperties": false,
          "x-parser-schema-id": "AirDifferentialPressureMetadata"
        },
        "x-parser-unique-object-id": "AirDifferentialPressureMsg",
        "x-parser-message-name": "AirDifferentialPressureMsg"
      },
      "AirFlowMsg": {
        "payload": {
          "allOf": [
            "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
            "$ref:$.channels.cduMetadata.messages.airFlow.payload.allOf[1]"
          ],
          "unevaluatedProperties": false,
          "x-parser-schema-id": "AirFlowMetadata"
        },
        "x-parser-unique-object-id": "AirFlowMsg",
        "x-parser-message-name": "AirFlowMsg"
      },
      "AirPressureMsg": {
        "payload": {
          "allOf": [
            "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
            "$ref:$.channels.cduMetadata.messages.airPressure.payload.allOf[1]"
          ],
          "unevaluatedProperties": false,
          "x-parser-schema-id": "AirPressureMetadata"
        },
        "x-parser-unique-object-id": "AirPressureMsg",
        "x-parser-message-name": "AirPressureMsg"
      },
      "LiquidTemperatureSpRequestMsg": "$ref:$.channels.cduMetadata.messages.liquidTemperatureSpRequest",
      "SoundMsg": {
        "payload": {
          "allOf": [
            "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
            "$ref:$.channels.sensorMetadata.messages.sound.payload.allOf[1]"
          ],
          "unevaluatedProperties": false,
          "x-parser-schema-id": "SoundMetadata"
        },
        "x-parser-unique-object-id": "SoundMsg",
        "x-parser-message-name": "SoundMsg"
      },
      "SystemHeartbeatTimestampBmsMsg": "$ref:$.channels.systemMetadata.messages.heartbeatTimestampBms",
      "SystemHeartbeatEchoBmsMsg": "$ref:$.channels.systemBmsValue.messages.heartbeatEchoBms",
      "SystemHearbeatTimestampIntegrationMsg": "$ref:$.channels.systemMetadata.messages.hearbeatTimestampIntegration",
      "SystemHeartbeatEchoIntegrationMsg": "$ref:$.channels.systemMetadata.messages.heartbeatEchoIntegration",
      "SystemStatusMsg": "$ref:$.channels.systemMetadata.messages.status",
      "SystemAvailableMsg": "$ref:$.channels.systemMetadata.messages.available",
      "BESSStatusMsg": "$ref:$.channels.bessMetadata.messages.status",
      "BESSAvailableMsg": "$ref:$.channels.bessMetadata.messages.available",
      "UPSStatusMsg": "$ref:$.channels.upsMetadata.messages.status",
      "UPSAvailableMsg": "$ref:$.channels.upsMetadata.messages.available",
      "ATSStatusMsg": "$ref:$.channels.atsMetadata.messages.status",
      "ATSAvailableMsg": "$ref:$.channels.atsMetadata.messages.available",
      "GeneratorStatusMsg": "$ref:$.channels.generatorMetadata.messages.status",
      "GeneratorAvailableMsg": "$ref:$.channels.generatorMetadata.messages.available",
      "ShuntStatusMsg": "$ref:$.channels.shuntMetadata.messages.status",
      "ShuntAvailableMsg": "$ref:$.channels.shuntMetadata.messages.available",
      "BreakerStatusMsg": "$ref:$.channels.breakerMetadata.messages.status",
      "BreakerAvailableMsg": "$ref:$.channels.breakerMetadata.messages.available",
      "ValveValvePositionMsg": "$ref:$.channels.valveMetadata.messages.valvePosition",
      "PumpPumpSpeedMsg": "$ref:$.channels.pumpMetadata.messages.pumpSpeed",
      "FanFanSpeedMsg": "$ref:$.channels.fanMetadata.messages.fanSpeed",
      "DamperDamperPositionMsg": "$ref:$.channels.damperMetadata.messages.damperPosition",
      "SensorLiquidTemperatureMsg": "$ref:$.channels.sensorMetadata.messages.liquidTemperature",
      "SensorLiquidDifferentialPressureMsg": "$ref:$.channels.sensorMetadata.messages.liquidDifferentialPressure",
      "SensorLiquidFlowMsg": "$ref:$.channels.sensorMetadata.messages.liquidFlow",
      "SensorLiquidPressureMsg": "$ref:$.channels.sensorMetadata.messages.liquidPressure",
      "SensorAirTemperatureMsg": "$ref:$.channels.sensorMetadata.messages.airTemperature",
      "SensorAirDifferentialPressureMsg": "$ref:$.channels.sensorMetadata.messages.airDifferentialPressure",
      "SensorAirFlowMsg": "$ref:$.channels.sensorMetadata.messages.airFlow",
      "SensorAirPressureMsg": "$ref:$.channels.sensorMetadata.messages.airPressure",
      "SensorSoundMsg": "$ref:$.channels.sensorMetadata.messages.sound",
      "ValveAvailableMsg": "$ref:$.channels.valveMetadata.messages.available",
      "PumpAvailableMsg": "$ref:$.channels.pumpMetadata.messages.available",
      "FanAvailableMsg": "$ref:$.channels.fanMetadata.messages.available",
      "DamperAvailableMsg": "$ref:$.channels.damperMetadata.messages.available",
      "SensorAvailableMsg": "$ref:$.channels.sensorMetadata.messages.available",
      "CDULeakDetectMsg": "$ref:$.channels.cduMetadata.messages.leakDetect",
      "CoolingTowerLeakDetectMsg": "$ref:$.channels.coolingTowerMetadata.messages.leakDetect",
      "HXLeakDetectMsg": "$ref:$.channels.hxMetadata.messages.leakDetect",
      "CRAHLeakDetectMsg": "$ref:$.channels.crahMetadata.messages.leakDetect",
      "CRACLeakDetectMsg": "$ref:$.channels.cracMetadata.messages.leakDetect",
      "AHULeakDetectMsg": "$ref:$.channels.ahuMetadata.messages.leakDetect",
      "ChillerLeakDetectMsg": "$ref:$.channels.chillerMetadata.messages.leakDetect",
      "TankLeakDetectMsg": "$ref:$.channels.tankMetadata.messages.leakDetect",
      "SensorLeakDetectMsg": "$ref:$.channels.sensorMetadata.messages.leakDetect",
      "GenericObjectLeakDetectMsg": "$ref:$.channels.genericObjectMetadata.messages.leakDetect",
      "CDUAirRelativeHumidityMsg": "$ref:$.channels.cduMetadata.messages.airRelativeHumidity",
      "CoolingTowerAirRelativeHumidityMsg": "$ref:$.channels.coolingTowerMetadata.messages.airRelativeHumidity",
      "HXAirRelativeHumidityMsg": "$ref:$.channels.hxMetadata.messages.airRelativeHumidity",
      "CRAHAirRelativeHumidityMsg": "$ref:$.channels.crahMetadata.messages.airRelativeHumidity",
      "CRACAirRelativeHumidityMsg": "$ref:$.channels.cracMetadata.messages.airRelativeHumidity",
      "AHUAirRelativeHumidityMsg": "$ref:$.channels.ahuMetadata.messages.airRelativeHumidity",
      "ChillerAirRelativeHumidityMsg": "$ref:$.channels.chillerMetadata.messages.airRelativeHumidity",
      "TankAirRelativeHumidityMsg": "$ref:$.channels.tankMetadata.messages.airRelativeHumidity",
      "SensorAirRelativeHumidityMsg": "$ref:$.channels.sensorMetadata.messages.airRelativeHumidity",
      "GenericObjectAirRelativeHumidityMsg": "$ref:$.channels.genericObjectMetadata.messages.airRelativeHumidity",
      "RackLiquidDifferentialPressureSpMsg": "$ref:$.channels.rackMetadata.messages.rackLiquidDifferentialPressureSp",
      "CDULiquidTemperatureMsg": "$ref:$.channels.cduMetadata.messages.liquidTemperature",
      "CDULiquidDifferentialPressureMsg": "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure",
      "CDULiquidFlowMsg": "$ref:$.channels.cduMetadata.messages.liquidFlow",
      "CDULiquidPressureMsg": "$ref:$.channels.cduMetadata.messages.liquidPressure",
      "CDUStatusMsg": "$ref:$.channels.cduMetadata.messages.status",
      "CDUAvailableMsg": "$ref:$.channels.cduMetadata.messages.available",
      "CDUValvePositionMsg": "$ref:$.channels.cduMetadata.messages.valvePosition",
      "CDUPumpSpeedMsg": "$ref:$.channels.cduMetadata.messages.pumpSpeed",
      "CDUFanSpeedMsg": "$ref:$.channels.cduMetadata.messages.fanSpeed",
      "CDUDamperPositionMsg": "$ref:$.channels.cduMetadata.messages.damperPosition",
      "CDUAirTemperatureMsg": "$ref:$.channels.cduMetadata.messages.airTemperature",
      "CDUAirDifferentialPressureMsg": "$ref:$.channels.cduMetadata.messages.airDifferentialPressure",
      "CDUAirFlowMsg": "$ref:$.channels.cduMetadata.messages.airFlow",
      "CDUAirPressureMsg": "$ref:$.channels.cduMetadata.messages.airPressure",
      "CoolingTowerLiquidTemperatureMsg": "$ref:$.channels.coolingTowerMetadata.messages.liquidTemperature",
      "CoolingTowerLiquidDifferentialPressureMsg": "$ref:$.channels.coolingTowerMetadata.messages.liquidDifferentialPressure",
      "CoolingTowerLiquidFlowMsg": "$ref:$.channels.coolingTowerMetadata.messages.liquidFlow",
      "CoolingTowerLiquidPressureMsg": "$ref:$.channels.coolingTowerMetadata.messages.liquidPressure",
      "CoolingTowerStatusMsg": "$ref:$.channels.coolingTowerMetadata.messages.status",
      "CoolingTowerAvailableMsg": "$ref:$.channels.coolingTowerMetadata.messages.available",
      "CoolingTowerValvePositionMsg": "$ref:$.channels.coolingTowerMetadata.messages.valvePosition",
      "CoolingTowerPumpSpeedMsg": "$ref:$.channels.coolingTowerMetadata.messages.pumpSpeed",
      "CoolingTowerFanSpeedMsg": "$ref:$.channels.coolingTowerMetadata.messages.fanSpeed",
      "CoolingTowerDamperPositionMsg": "$ref:$.channels.coolingTowerMetadata.messages.damperPosition",
      "CoolingTowerAirTemperatureMsg": "$ref:$.channels.coolingTowerMetadata.messages.airTemperature",
      "CoolingTowerAirDifferentialPressureMsg": "$ref:$.channels.coolingTowerMetadata.messages.airDifferentialPressure",
      "CoolingTowerAirFlowMsg": "$ref:$.channels.coolingTowerMetadata.messages.airFlow",
      "CoolingTowerAirPressureMsg": "$ref:$.channels.coolingTowerMetadata.messages.airPressure",
      "HXLiquidTemperatureMsg": "$ref:$.channels.hxMetadata.messages.liquidTemperature",
      "HXLiquidDifferentialPressureMsg": "$ref:$.channels.hxMetadata.messages.liquidDifferentialPressure",
      "HXLiquidFlowMsg": "$ref:$.channels.hxMetadata.messages.liquidFlow",
      "HXLiquidPressureMsg": "$ref:$.channels.hxMetadata.messages.liquidPressure",
      "HXStatusMsg": "$ref:$.channels.hxMetadata.messages.status",
      "HXAvailableMsg": "$ref:$.channels.hxMetadata.messages.available",
      "HXValvePositionMsg": "$ref:$.channels.hxMetadata.messages.valvePosition",
      "HXPumpSpeedMsg": "$ref:$.channels.hxMetadata.messages.pumpSpeed",
      "HXFanSpeedMsg": "$ref:$.channels.hxMetadata.messages.fanSpeed",
      "HXDamperPositionMsg": "$ref:$.channels.hxMetadata.messages.damperPosition",
      "HXAirTemperatureMsg": "$ref:$.channels.hxMetadata.messages.airTemperature",
      "HXAirDifferentialPressureMsg": "$ref:$.channels.hxMetadata.messages.airDifferentialPressure",
      "HXAirFlowMsg": "$ref:$.channels.hxMetadata.messages.airFlow",
      "HXAirPressureMsg": "$ref:$.channels.hxMetadata.messages.airPressure",
      "CRAHLiquidTemperatureMsg": "$ref:$.channels.crahMetadata.messages.liquidTemperature",
      "CRAHLiquidDifferentialPressureMsg": "$ref:$.channels.crahMetadata.messages.liquidDifferentialPressure",
      "CRAHLiquidFlowMsg": "$ref:$.channels.crahMetadata.messages.liquidFlow",
      "CRAHLiquidPressureMsg": "$ref:$.channels.crahMetadata.messages.liquidPressure",
      "CRAHStatusMsg": "$ref:$.channels.crahMetadata.messages.status",
      "CRAHAvailableMsg": "$ref:$.channels.crahMetadata.messages.available",
      "CRAHValvePositionMsg": "$ref:$.channels.crahMetadata.messages.valvePosition",
      "CRAHPumpSpeedMsg": "$ref:$.channels.crahMetadata.messages.pumpSpeed",
      "CRAHFanSpeedMsg": "$ref:$.channels.crahMetadata.messages.fanSpeed",
      "CRAHDamperPositionMsg": "$ref:$.channels.crahMetadata.messages.damperPosition",
      "CRAHAirTemperatureMsg": "$ref:$.channels.crahMetadata.messages.airTemperature",
      "CRAHAirDifferentialPressureMsg": "$ref:$.channels.crahMetadata.messages.airDifferentialPressure",
      "CRAHAirFlowMsg": "$ref:$.channels.crahMetadata.messages.airFlow",
      "CRAHAirPressureMsg": "$ref:$.channels.crahMetadata.messages.airPressure",
      "CRACLiquidTemperatureMsg": "$ref:$.channels.cracMetadata.messages.liquidTemperature",
      "CRACLiquidDifferentialPressureMsg": "$ref:$.channels.cracMetadata.messages.liquidDifferentialPressure",
      "CRACLiquidFlowMsg": "$ref:$.channels.cracMetadata.messages.liquidFlow",
      "CRACLiquidPressureMsg": "$ref:$.channels.cracMetadata.messages.liquidPressure",
      "CRACStatusMsg": "$ref:$.channels.cracMetadata.messages.status",
      "CRACAvailableMsg": "$ref:$.channels.cracMetadata.messages.available",
      "CRACValvePositionMsg": "$ref:$.channels.cracMetadata.messages.valvePosition",
      "CRACPumpSpeedMsg": "$ref:$.channels.cracMetadata.messages.pumpSpeed",
      "CRACFanSpeedMsg": "$ref:$.channels.cracMetadata.messages.fanSpeed",
      "CRACDamperPositionMsg": "$ref:$.channels.cracMetadata.messages.damperPosition",
      "CRACAirTemperatureMsg": "$ref:$.channels.cracMetadata.messages.airTemperature",
      "CRACAirDifferentialPressureMsg": "$ref:$.channels.cracMetadata.messages.airDifferentialPressure",
      "CRACAirFlowMsg": "$ref:$.channels.cracMetadata.messages.airFlow",
      "CRACAirPressureMsg": "$ref:$.channels.cracMetadata.messages.airPressure",
      "AHULiquidTemperatureMsg": "$ref:$.channels.ahuMetadata.messages.liquidTemperature",
      "AHULiquidDifferentialPressureMsg": "$ref:$.channels.ahuMetadata.messages.liquidDifferentialPressure",
      "AHULiquidFlowMsg": "$ref:$.channels.ahuMetadata.messages.liquidFlow",
      "AHULiquidPressureMsg": "$ref:$.channels.ahuMetadata.messages.liquidPressure",
      "AHUStatusMsg": "$ref:$.channels.ahuMetadata.messages.status",
      "AHUAvailableMsg": "$ref:$.channels.ahuMetadata.messages.available",
      "AHUValvePositionMsg": "$ref:$.channels.ahuMetadata.messages.valvePosition",
      "AHUPumpSpeedMsg": "$ref:$.channels.ahuMetadata.messages.pumpSpeed",
      "AHUFanSpeedMsg": "$ref:$.channels.ahuMetadata.messages.fanSpeed",
      "AHUDamperPositionMsg": "$ref:$.channels.ahuMetadata.messages.damperPosition",
      "AHUAirTemperatureMsg": "$ref:$.channels.ahuMetadata.messages.airTemperature",
      "AHUAirDifferentialPressureMsg": "$ref:$.channels.ahuMetadata.messages.airDifferentialPressure",
      "AHUAirFlowMsg": "$ref:$.channels.ahuMetadata.messages.airFlow",
      "AHUAirPressureMsg": "$ref:$.channels.ahuMetadata.messages.airPressure",
      "ChillerLiquidTemperatureMsg": "$ref:$.channels.chillerMetadata.messages.liquidTemperature",
      "ChillerLiquidDifferentialPressureMsg": "$ref:$.channels.chillerMetadata.messages.liquidDifferentialPressure",
      "ChillerLiquidFlowMsg": "$ref:$.channels.chillerMetadata.messages.liquidFlow",
      "ChillerLiquidPressureMsg": "$ref:$.channels.chillerMetadata.messages.liquidPressure",
      "ChillerStatusMsg": "$ref:$.channels.chillerMetadata.messages.status",
      "ChillerAvailableMsg": "$ref:$.channels.chillerMetadata.messages.available",
      "ChillerValvePositionMsg": "$ref:$.channels.chillerMetadata.messages.valvePosition",
      "ChillerPumpSpeedMsg": "$ref:$.channels.chillerMetadata.messages.pumpSpeed",
      "ChillerFanSpeedMsg": "$ref:$.channels.chillerMetadata.messages.fanSpeed",
      "ChillerDamperPositionMsg": "$ref:$.channels.chillerMetadata.messages.damperPosition",
      "ChillerAirTemperatureMsg": "$ref:$.channels.chillerMetadata.messages.airTemperature",
      "ChillerAirDifferentialPressureMsg": "$ref:$.channels.chillerMetadata.messages.airDifferentialPressure",
      "ChillerAirFlowMsg": "$ref:$.channels.chillerMetadata.messages.airFlow",
      "ChillerAirPressureMsg": "$ref:$.channels.chillerMetadata.messages.airPressure",
      "GenericEquipmentPointMsg": "$ref:$.channels.bessMetadata.messages.genericPoint",
      "GenericPowerMeterPointMsg": "$ref:$.channels.powerMeterMetadata.messages.genericPoint",
      "TankLiquidTemperatureMsg": "$ref:$.channels.tankMetadata.messages.liquidTemperature",
      "TankLiquidDifferentialPressureMsg": "$ref:$.channels.tankMetadata.messages.liquidDifferentialPressure",
      "TankLiquidFlowMsg": "$ref:$.channels.tankMetadata.messages.liquidFlow",
      "TankLiquidPressureMsg": "$ref:$.channels.tankMetadata.messages.liquidPressure",
      "TankStatusMsg": "$ref:$.channels.tankMetadata.messages.status",
      "TankAvailableMsg": "$ref:$.channels.tankMetadata.messages.available",
      "TankValvePositionMsg": "$ref:$.channels.tankMetadata.messages.valvePosition",
      "TankPumpSpeedMsg": "$ref:$.channels.tankMetadata.messages.pumpSpeed",
      "TankFanSpeedMsg": "$ref:$.channels.tankMetadata.messages.fanSpeed",
      "TankDamperPositionMsg": "$ref:$.channels.tankMetadata.messages.damperPosition",
      "TankAirTemperatureMsg": "$ref:$.channels.tankMetadata.messages.airTemperature",
      "TankAirDifferentialPressureMsg": "$ref:$.channels.tankMetadata.messages.airDifferentialPressure",
      "TankAirFlowMsg": "$ref:$.channels.tankMetadata.messages.airFlow",
      "TankAirPressureMsg": "$ref:$.channels.tankMetadata.messages.airPressure",
      "GenericObjectLiquidTemperatureMsg": "$ref:$.channels.genericObjectMetadata.messages.liquidTemperature",
      "GenericObjectLiquidDifferentialPressureMsg": "$ref:$.channels.genericObjectMetadata.messages.liquidDifferentialPressure",
      "GenericObjectLiquidFlowMsg": "$ref:$.channels.genericObjectMetadata.messages.liquidFlow",
      "GenericObjectLiquidPressureMsg": "$ref:$.channels.genericObjectMetadata.messages.liquidPressure",
      "GenericObjectStatusMsg": "$ref:$.channels.genericObjectMetadata.messages.status",
      "GenericObjectAvailableMsg": "$ref:$.channels.genericObjectMetadata.messages.available",
      "GenericObjectValvePositionMsg": "$ref:$.channels.genericObjectMetadata.messages.valvePosition",
      "GenericObjectPumpSpeedMsg": "$ref:$.channels.genericObjectMetadata.messages.pumpSpeed",
      "GenericObjectFanSpeedMsg": "$ref:$.channels.genericObjectMetadata.messages.fanSpeed",
      "GenericObjectDamperPositionMsg": "$ref:$.channels.genericObjectMetadata.messages.damperPosition",
      "GenericObjectAirTemperatureMsg": "$ref:$.channels.genericObjectMetadata.messages.airTemperature",
      "GenericObjectAirDifferentialPressureMsg": "$ref:$.channels.genericObjectMetadata.messages.airDifferentialPressure",
      "GenericObjectAirFlowMsg": "$ref:$.channels.genericObjectMetadata.messages.airFlow",
      "GenericObjectAirPressureMsg": "$ref:$.channels.genericObjectMetadata.messages.airPressure",
      "GenericObjectLiquidTemperatureSpRequestMsg": "$ref:$.channels.genericObjectMetadata.messages.liquidTemperatureSpRequest",
      "GenericObjectSoundMsg": "$ref:$.channels.genericObjectMetadata.messages.sound"
    },
    "schemas": {
      "MetadataBase": "$ref:$.channels.rackMetadata.messages.rackLiquidSupplyTemperature.payload.allOf[0].allOf[0]",
      "IntegrationPublisherFields": "$ref:$.channels.rackMetadata.messages.rackLiquidIsolationRequest.payload.allOf[0].allOf[1]",
      "StateTextField": "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0].allOf[3].oneOf[1]",
      "EquipmentPointEngUnit": "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0].allOf[3].oneOf[0]",
      "EquipmentMeasurementModeBase": "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[0]",
      "rackLocationIdentifiers": "$ref:$.channels.rackMetadata.messages.rackLiquidSupplyTemperature.payload.allOf[0].allOf[1]",
      "PowerMeterIdentifiers": "$ref:$.channels.powerMeterMetadata.messages.voltage.payload.allOf[0].allOf[1]",
      "EquipmentCommonFields": "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0].allOf[1]",
      "EquipmentNamedObjectMode": "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0].allOf[2].oneOf[0]",
      "EquipmentAssociateMode": "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0].allOf[2].oneOf[1]",
      "EquipmentIntegrationIdentifierFields": "$ref:$.channels.cduMetadata.messages.liquidTemperatureSpRequest.payload.allOf[0].allOf[1]",
      "SystemIntegrationIdentifiers": "$ref:$.channels.systemMetadata.messages.hearbeatTimestampIntegration.payload.allOf[0].allOf[1]",
      "RackMetadataBase": "$ref:$.channels.rackMetadata.messages.rackLiquidSupplyTemperature.payload.allOf[0]",
      "RackIntegrationMetadataBase": "$ref:$.channels.rackMetadata.messages.rackLiquidIsolationRequest.payload.allOf[0]",
      "PowerMeterMetadataBase": "$ref:$.channels.powerMeterMetadata.messages.voltage.payload.allOf[0]",
      "EquipmentMetadataBase": "$ref:$.channels.bessMetadata.messages.status.payload.allOf[0]",
      "EquipmentIntegrationMetadataBase": "$ref:$.channels.cduMetadata.messages.liquidTemperatureSpRequest.payload.allOf[0]",
      "SystemIntegrationMetadataBase": "$ref:$.channels.systemMetadata.messages.hearbeatTimestampIntegration.payload.allOf[0]",
      "RackBmsPointType": {
        "type": "string",
        "description": "Valid pointType values for BMS-published Rack points.",
        "enum": [
          "RackLiquidSupplyTemperature",
          "RackLiquidReturnTemperature",
          "RackLiquidFlow",
          "RackLiquidDifferentialPressure",
          "RackControlValvePosition",
          "RackPower",
          "RackLeakDetect",
          "RackLeakSensorFault",
          "RackLiquidIsolationStatus",
          "RackElectricalIsolationStatus"
        ],
        "x-parser-schema-id": "RackBmsPointType"
      },
      "RackIntegrationPointType": {
        "type": "string",
        "description": "Valid pointType values for Integration-published Rack points.",
        "enum": [
          "RackLeakDetectTray",
          "RackLiquidIsolationRequest",
          "RackElectricalIsolationRequest"
        ],
        "x-parser-schema-id": "RackIntegrationPointType"
      },
      "PowerMeterPointType": {
        "type": "string",
        "description": "Valid pointType values for PowerMeter points (all BMS-published).",
        "enum": [
          "Voltage",
          "PowerFactor",
          "Frequency",
          "ApparentPower",
          "ActivePower",
          "Current",
          "CurrentLimit",
          "PhaseCurrent"
        ],
        "x-parser-schema-id": "PowerMeterPointType"
      },
      "EquipmentBmsPointType": {
        "type": "string",
        "description": "Valid pointType values for BMS-published generic equipment points.",
        "enum": [
          "LiquidTemperature",
          "LiquidDifferentialPressure",
          "LiquidFlow",
          "LiquidPressure",
          "Status",
          "Available",
          "ValvePosition",
          "PumpSpeed",
          "FanSpeed",
          "DamperPosition",
          "AirTemperature",
          "AirDifferentialPressure",
          "AirFlow",
          "AirPressure",
          "Sound",
          "GenericPoint"
        ],
        "x-parser-schema-id": "EquipmentBmsPointType"
      },
      "EquipmentIntegrationPointType": {
        "type": "string",
        "description": "Valid pointType values for Integration-published generic equipment points.",
        "enum": [
          "LiquidTemperatureSpRequest"
        ],
        "x-parser-schema-id": "EquipmentIntegrationPointType"
      },
      "EquipmentObjectType": {
        "type": "string",
        "description": "Valid objectType values for generic equipment channels.",
        "enum": [
          "CDU",
          "CoolingTower",
          "HX",
          "CRAH",
          "CRAC",
          "AHU",
          "Chiller",
          "BESS",
          "UPS",
          "ATS",
          "Generator",
          "Shunt",
          "Breaker",
          "Valve",
          "Pump",
          "Fan",
          "Damper",
          "Sensor",
          "Tank",
          "GenericObject"
        ],
        "x-parser-schema-id": "EquipmentObjectType"
      },
      "SystemBmsPointType": {
        "type": "string",
        "description": "Valid pointType values for BMS-published System points.",
        "enum": [
          "HeartbeatTimestampBms",
          "HeartbeatEchoBms",
          "Status",
          "Available",
          "GenericPoint"
        ],
        "x-parser-schema-id": "SystemBmsPointType"
      },
      "SystemIntegrationPointType": {
        "type": "string",
        "description": "Valid pointType values for Integration-published System points.",
        "enum": [
          "HearbeatTimestampIntegration",
          "HeartbeatEchoIntegration"
        ],
        "x-parser-schema-id": "SystemIntegrationPointType"
      },
      "RackLiquidSupplyTemperatureFields": "$ref:$.channels.rackMetadata.messages.rackLiquidSupplyTemperature.payload.allOf[1]",
      "RackLiquidReturnTemperatureFields": "$ref:$.channels.rackMetadata.messages.rackLiquidReturnTemperature.payload.allOf[1]",
      "RackLiquidFlowFields": "$ref:$.channels.rackMetadata.messages.rackLiquidFlow.payload.allOf[1]",
      "RackLiquidDifferentialPressureFields": "$ref:$.channels.rackMetadata.messages.rackLiquidDifferentialPressure.payload.allOf[1]",
      "RackControlValvePositionFields": "$ref:$.channels.rackMetadata.messages.rackControlValvePosition.payload.allOf[1]",
      "RackPowerFields": "$ref:$.channels.rackMetadata.messages.rackPower.payload.allOf[1]",
      "RackLeakDetectFields": "$ref:$.channels.rackMetadata.messages.rackLeakDetect.payload.allOf[1]",
      "RackLeakSensorFaultFields": "$ref:$.channels.rackMetadata.messages.rackLeakSensorFault.payload.allOf[1]",
      "RackLiquidIsolationStatusFields": "$ref:$.channels.rackMetadata.messages.rackLiquidIsolationStatus.payload.allOf[1]",
      "RackElectricalIsolationStatusFields": "$ref:$.channels.rackMetadata.messages.rackElectricalIsolationStatus.payload.allOf[1]",
      "RackLeakDetectTrayFields": "$ref:$.channels.rackMetadata.messages.rackLeakDetectTray.payload.allOf[1]",
      "RackLiquidIsolationRequestFields": "$ref:$.channels.rackMetadata.messages.rackLiquidIsolationRequest.payload.allOf[1]",
      "RackElectricalIsolationRequestFields": "$ref:$.channels.rackMetadata.messages.rackElectricalIsolationRequest.payload.allOf[1]",
      "VoltageFields": "$ref:$.channels.powerMeterMetadata.messages.voltage.payload.allOf[1]",
      "PowerFactorFields": "$ref:$.channels.powerMeterMetadata.messages.powerFactor.payload.allOf[1]",
      "FrequencyFields": "$ref:$.channels.powerMeterMetadata.messages.frequency.payload.allOf[1]",
      "ApparentPowerFields": "$ref:$.channels.powerMeterMetadata.messages.apparentPower.payload.allOf[1]",
      "ActivePowerFields": "$ref:$.channels.powerMeterMetadata.messages.activePower.payload.allOf[1]",
      "CurrentFields": "$ref:$.channels.powerMeterMetadata.messages.current.payload.allOf[1]",
      "CurrentLimitFields": "$ref:$.channels.powerMeterMetadata.messages.currentLimit.payload.allOf[1]",
      "PhaseCurrentFields": "$ref:$.channels.powerMeterMetadata.messages.phaseCurrent.payload.allOf[1]",
      "LiquidTemperatureFields": "$ref:$.channels.cduMetadata.messages.liquidTemperature.payload.allOf[1]",
      "LiquidDifferentialPressureFields": "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload.allOf[1]",
      "LiquidFlowFields": "$ref:$.channels.cduMetadata.messages.liquidFlow.payload.allOf[1]",
      "LiquidPressureFields": "$ref:$.channels.cduMetadata.messages.liquidPressure.payload.allOf[1]",
      "StatusFields": "$ref:$.channels.bessMetadata.messages.status.payload.allOf[1]",
      "AvailableFields": "$ref:$.channels.bessMetadata.messages.available.payload.allOf[1]",
      "ValvePositionFields": "$ref:$.channels.cduMetadata.messages.valvePosition.payload.allOf[1]",
      "PumpSpeedFields": "$ref:$.channels.cduMetadata.messages.pumpSpeed.payload.allOf[1]",
      "FanSpeedFields": "$ref:$.channels.cduMetadata.messages.fanSpeed.payload.allOf[1]",
      "DamperPositionFields": "$ref:$.channels.cduMetadata.messages.damperPosition.payload.allOf[1]",
      "AirTemperatureFields": "$ref:$.channels.cduMetadata.messages.airTemperature.payload.allOf[1]",
      "AirDifferentialPressureFields": "$ref:$.channels.cduMetadata.messages.airDifferentialPressure.payload.allOf[1]",
      "AirFlowFields": "$ref:$.channels.cduMetadata.messages.airFlow.payload.allOf[1]",
      "AirPressureFields": "$ref:$.channels.cduMetadata.messages.airPressure.payload.allOf[1]",
      "LeakDetectFields": "$ref:$.channels.cduMetadata.messages.leakDetect.payload.allOf[1]",
      "AirRelativeHumidityFields": "$ref:$.channels.cduMetadata.messages.airRelativeHumidity.payload.allOf[1]",
      "LiquidTemperatureSpRequestFields": "$ref:$.channels.cduMetadata.messages.liquidTemperatureSpRequest.payload.allOf[1]",
      "GenericObjectLiquidTemperatureSpRequestFields": "$ref:$.channels.genericObjectMetadata.messages.liquidTemperatureSpRequest.payload.allOf[1]",
      "SoundFields": "$ref:$.channels.sensorMetadata.messages.sound.payload.allOf[1]",
      "HeartbeatTimestampBmsFields": "$ref:$.channels.systemMetadata.messages.heartbeatTimestampBms.payload.allOf[1]",
      "HeartbeatEchoBmsFields": "$ref:$.channels.systemBmsValue.messages.heartbeatEchoBms.payload.allOf[1]",
      "HearbeatTimestampIntegrationFields": "$ref:$.channels.systemMetadata.messages.hearbeatTimestampIntegration.payload.allOf[1]",
      "HeartbeatEchoIntegrationFields": "$ref:$.channels.systemMetadata.messages.heartbeatEchoIntegration.payload.allOf[1]",
      "RackLiquidSupplyTemperatureMetadata": "$ref:$.channels.rackMetadata.messages.rackLiquidSupplyTemperature.payload",
      "RackLiquidReturnTemperatureMetadata": "$ref:$.channels.rackMetadata.messages.rackLiquidReturnTemperature.payload",
      "RackLiquidFlowMetadata": "$ref:$.channels.rackMetadata.messages.rackLiquidFlow.payload",
      "RackLiquidDifferentialPressureMetadata": "$ref:$.channels.rackMetadata.messages.rackLiquidDifferentialPressure.payload",
      "RackLiquidDifferentialPressureSpFields": "$ref:$.channels.rackMetadata.messages.rackLiquidDifferentialPressureSp.payload.allOf[1]",
      "RackLiquidDifferentialPressureSpMetadata": "$ref:$.channels.rackMetadata.messages.rackLiquidDifferentialPressureSp.payload",
      "RackControlValvePositionMetadata": "$ref:$.channels.rackMetadata.messages.rackControlValvePosition.payload",
      "RackPowerMetadata": "$ref:$.channels.rackMetadata.messages.rackPower.payload",
      "RackLeakDetectMetadata": "$ref:$.channels.rackMetadata.messages.rackLeakDetect.payload",
      "RackLeakSensorFaultMetadata": "$ref:$.channels.rackMetadata.messages.rackLeakSensorFault.payload",
      "RackLiquidIsolationStatusMetadata": "$ref:$.channels.rackMetadata.messages.rackLiquidIsolationStatus.payload",
      "RackElectricalIsolationStatusMetadata": "$ref:$.channels.rackMetadata.messages.rackElectricalIsolationStatus.payload",
      "RackLeakDetectTrayMetadata": "$ref:$.channels.rackMetadata.messages.rackLeakDetectTray.payload",
      "RackLiquidIsolationRequestMetadata": "$ref:$.channels.rackMetadata.messages.rackLiquidIsolationRequest.payload",
      "RackElectricalIsolationRequestMetadata": "$ref:$.channels.rackMetadata.messages.rackElectricalIsolationRequest.payload",
      "PowerMeterVoltageMetadata": "$ref:$.channels.powerMeterMetadata.messages.voltage.payload",
      "PowerMeterPowerFactorMetadata": "$ref:$.channels.powerMeterMetadata.messages.powerFactor.payload",
      "PowerMeterFrequencyMetadata": "$ref:$.channels.powerMeterMetadata.messages.frequency.payload",
      "PowerMeterApparentPowerMetadata": "$ref:$.channels.powerMeterMetadata.messages.apparentPower.payload",
      "PowerMeterActivePowerMetadata": "$ref:$.channels.powerMeterMetadata.messages.activePower.payload",
      "PowerMeterCurrentMetadata": "$ref:$.channels.powerMeterMetadata.messages.current.payload",
      "PowerMeterCurrentLimitMetadata": "$ref:$.channels.powerMeterMetadata.messages.currentLimit.payload",
      "PowerMeterPhaseCurrentMetadata": "$ref:$.channels.powerMeterMetadata.messages.phaseCurrent.payload",
      "LiquidTemperatureMetadata": "$ref:$.components.messages.LiquidTemperatureMsg.payload",
      "LiquidDifferentialPressureMetadata": "$ref:$.components.messages.LiquidDifferentialPressureMsg.payload",
      "LiquidFlowMetadata": "$ref:$.components.messages.LiquidFlowMsg.payload",
      "LiquidPressureMetadata": "$ref:$.components.messages.LiquidPressureMsg.payload",
      "StatusMetadata": "$ref:$.components.messages.StatusMsg.payload",
      "AvailableMetadata": "$ref:$.components.messages.AvailableMsg.payload",
      "ValvePositionMetadata": "$ref:$.components.messages.ValvePositionMsg.payload",
      "PumpSpeedMetadata": "$ref:$.components.messages.PumpSpeedMsg.payload",
      "FanSpeedMetadata": "$ref:$.components.messages.FanSpeedMsg.payload",
      "DamperPositionMetadata": "$ref:$.components.messages.DamperPositionMsg.payload",
      "AirTemperatureMetadata": "$ref:$.components.messages.AirTemperatureMsg.payload",
      "AirDifferentialPressureMetadata": "$ref:$.components.messages.AirDifferentialPressureMsg.payload",
      "AirFlowMetadata": "$ref:$.components.messages.AirFlowMsg.payload",
      "AirPressureMetadata": "$ref:$.components.messages.AirPressureMsg.payload",
      "SoundMetadata": "$ref:$.components.messages.SoundMsg.payload",
      "LiquidTemperatureSpRequestMetadata": "$ref:$.channels.cduMetadata.messages.liquidTemperatureSpRequest.payload",
      "SystemHeartbeatTimestampBmsMetadata": "$ref:$.channels.systemMetadata.messages.heartbeatTimestampBms.payload",
      "SystemHeartbeatEchoBmsMetadata": "$ref:$.channels.systemBmsValue.messages.heartbeatEchoBms.payload",
      "SystemHearbeatTimestampIntegrationMetadata": "$ref:$.channels.systemMetadata.messages.hearbeatTimestampIntegration.payload",
      "SystemHeartbeatEchoIntegrationMetadata": "$ref:$.channels.systemMetadata.messages.heartbeatEchoIntegration.payload",
      "SystemObjectTypeFields": "$ref:$.channels.systemMetadata.messages.status.payload.allOf[2]",
      "SystemStatusMetadata": "$ref:$.channels.systemMetadata.messages.status.payload",
      "SystemAvailableMetadata": "$ref:$.channels.systemMetadata.messages.available.payload",
      "BESSObjectTypeFields": "$ref:$.channels.bessMetadata.messages.status.payload.allOf[2]",
      "UPSObjectTypeFields": "$ref:$.channels.upsMetadata.messages.status.payload.allOf[2]",
      "ATSObjectTypeFields": "$ref:$.channels.atsMetadata.messages.status.payload.allOf[2]",
      "GeneratorObjectTypeFields": "$ref:$.channels.generatorMetadata.messages.status.payload.allOf[2]",
      "ShuntObjectTypeFields": "$ref:$.channels.shuntMetadata.messages.status.payload.allOf[2]",
      "BreakerObjectTypeFields": "$ref:$.channels.breakerMetadata.messages.status.payload.allOf[2]",
      "CDUObjectTypeFields": "$ref:$.channels.cduMetadata.messages.liquidTemperature.payload.allOf[2]",
      "CoolingTowerObjectTypeFields": "$ref:$.channels.coolingTowerMetadata.messages.liquidTemperature.payload.allOf[2]",
      "HXObjectTypeFields": "$ref:$.channels.hxMetadata.messages.liquidTemperature.payload.allOf[2]",
      "CRAHObjectTypeFields": "$ref:$.channels.crahMetadata.messages.liquidTemperature.payload.allOf[2]",
      "CRACObjectTypeFields": "$ref:$.channels.cracMetadata.messages.liquidTemperature.payload.allOf[2]",
      "AHUObjectTypeFields": "$ref:$.channels.ahuMetadata.messages.liquidTemperature.payload.allOf[2]",
      "ChillerObjectTypeFields": "$ref:$.channels.chillerMetadata.messages.liquidTemperature.payload.allOf[2]",
      "ValveObjectTypeFields": "$ref:$.channels.valveMetadata.messages.valvePosition.payload.allOf[2]",
      "PumpObjectTypeFields": "$ref:$.channels.pumpMetadata.messages.pumpSpeed.payload.allOf[2]",
      "FanObjectTypeFields": "$ref:$.channels.fanMetadata.messages.fanSpeed.payload.allOf[2]",
      "DamperObjectTypeFields": "$ref:$.channels.damperMetadata.messages.damperPosition.payload.allOf[2]",
      "SensorObjectTypeFields": "$ref:$.channels.sensorMetadata.messages.liquidTemperature.payload.allOf[2]",
      "TankObjectTypeFields": "$ref:$.channels.tankMetadata.messages.liquidTemperature.payload.allOf[2]",
      "GenericObjectObjectTypeFields": "$ref:$.channels.genericObjectMetadata.messages.liquidTemperature.payload.allOf[2]",
      "BESSStatusMetadata": "$ref:$.channels.bessMetadata.messages.status.payload",
      "BESSAvailableMetadata": "$ref:$.channels.bessMetadata.messages.available.payload",
      "UPSStatusMetadata": "$ref:$.channels.upsMetadata.messages.status.payload",
      "UPSAvailableMetadata": "$ref:$.channels.upsMetadata.messages.available.payload",
      "ATSStatusMetadata": "$ref:$.channels.atsMetadata.messages.status.payload",
      "ATSAvailableMetadata": "$ref:$.channels.atsMetadata.messages.available.payload",
      "GeneratorStatusMetadata": "$ref:$.channels.generatorMetadata.messages.status.payload",
      "GeneratorAvailableMetadata": "$ref:$.channels.generatorMetadata.messages.available.payload",
      "ShuntStatusMetadata": "$ref:$.channels.shuntMetadata.messages.status.payload",
      "ShuntAvailableMetadata": "$ref:$.channels.shuntMetadata.messages.available.payload",
      "BreakerStatusMetadata": "$ref:$.channels.breakerMetadata.messages.status.payload",
      "BreakerAvailableMetadata": "$ref:$.channels.breakerMetadata.messages.available.payload",
      "ValveValvePositionMetadata": "$ref:$.channels.valveMetadata.messages.valvePosition.payload",
      "PumpPumpSpeedMetadata": "$ref:$.channels.pumpMetadata.messages.pumpSpeed.payload",
      "FanFanSpeedMetadata": "$ref:$.channels.fanMetadata.messages.fanSpeed.payload",
      "DamperDamperPositionMetadata": "$ref:$.channels.damperMetadata.messages.damperPosition.payload",
      "ValveAvailableMetadata": "$ref:$.channels.valveMetadata.messages.available.payload",
      "PumpAvailableMetadata": "$ref:$.channels.pumpMetadata.messages.available.payload",
      "FanAvailableMetadata": "$ref:$.channels.fanMetadata.messages.available.payload",
      "DamperAvailableMetadata": "$ref:$.channels.damperMetadata.messages.available.payload",
      "SensorAvailableMetadata": "$ref:$.channels.sensorMetadata.messages.available.payload",
      "SensorLiquidTemperatureMetadata": "$ref:$.channels.sensorMetadata.messages.liquidTemperature.payload",
      "SensorLiquidDifferentialPressureMetadata": "$ref:$.channels.sensorMetadata.messages.liquidDifferentialPressure.payload",
      "SensorLiquidFlowMetadata": "$ref:$.channels.sensorMetadata.messages.liquidFlow.payload",
      "SensorLiquidPressureMetadata": "$ref:$.channels.sensorMetadata.messages.liquidPressure.payload",
      "SensorAirTemperatureMetadata": "$ref:$.channels.sensorMetadata.messages.airTemperature.payload",
      "SensorAirDifferentialPressureMetadata": "$ref:$.channels.sensorMetadata.messages.airDifferentialPressure.payload",
      "SensorAirFlowMetadata": "$ref:$.channels.sensorMetadata.messages.airFlow.payload",
      "SensorAirPressureMetadata": "$ref:$.channels.sensorMetadata.messages.airPressure.payload",
      "SensorSoundMetadata": "$ref:$.channels.sensorMetadata.messages.sound.payload",
      "SensorLeakDetectMetadata": "$ref:$.channels.sensorMetadata.messages.leakDetect.payload",
      "SensorAirRelativeHumidityMetadata": "$ref:$.channels.sensorMetadata.messages.airRelativeHumidity.payload",
      "CDULiquidTemperatureMetadata": "$ref:$.channels.cduMetadata.messages.liquidTemperature.payload",
      "CDULiquidDifferentialPressureMetadata": "$ref:$.channels.cduMetadata.messages.liquidDifferentialPressure.payload",
      "CDULiquidFlowMetadata": "$ref:$.channels.cduMetadata.messages.liquidFlow.payload",
      "CDULiquidPressureMetadata": "$ref:$.channels.cduMetadata.messages.liquidPressure.payload",
      "CDUStatusMetadata": "$ref:$.channels.cduMetadata.messages.status.payload",
      "CDUAvailableMetadata": "$ref:$.channels.cduMetadata.messages.available.payload",
      "CDUValvePositionMetadata": "$ref:$.channels.cduMetadata.messages.valvePosition.payload",
      "CDUPumpSpeedMetadata": "$ref:$.channels.cduMetadata.messages.pumpSpeed.payload",
      "CDUFanSpeedMetadata": "$ref:$.channels.cduMetadata.messages.fanSpeed.payload",
      "CDUDamperPositionMetadata": "$ref:$.channels.cduMetadata.messages.damperPosition.payload",
      "CDUAirTemperatureMetadata": "$ref:$.channels.cduMetadata.messages.airTemperature.payload",
      "CDUAirDifferentialPressureMetadata": "$ref:$.channels.cduMetadata.messages.airDifferentialPressure.payload",
      "CDUAirFlowMetadata": "$ref:$.channels.cduMetadata.messages.airFlow.payload",
      "CDUAirPressureMetadata": "$ref:$.channels.cduMetadata.messages.airPressure.payload",
      "CDULeakDetectMetadata": "$ref:$.channels.cduMetadata.messages.leakDetect.payload",
      "CDUAirRelativeHumidityMetadata": "$ref:$.channels.cduMetadata.messages.airRelativeHumidity.payload",
      "CoolingTowerLiquidTemperatureMetadata": "$ref:$.channels.coolingTowerMetadata.messages.liquidTemperature.payload",
      "CoolingTowerLiquidDifferentialPressureMetadata": "$ref:$.channels.coolingTowerMetadata.messages.liquidDifferentialPressure.payload",
      "CoolingTowerLiquidFlowMetadata": "$ref:$.channels.coolingTowerMetadata.messages.liquidFlow.payload",
      "CoolingTowerLiquidPressureMetadata": "$ref:$.channels.coolingTowerMetadata.messages.liquidPressure.payload",
      "CoolingTowerStatusMetadata": "$ref:$.channels.coolingTowerMetadata.messages.status.payload",
      "CoolingTowerAvailableMetadata": "$ref:$.channels.coolingTowerMetadata.messages.available.payload",
      "CoolingTowerValvePositionMetadata": "$ref:$.channels.coolingTowerMetadata.messages.valvePosition.payload",
      "CoolingTowerPumpSpeedMetadata": "$ref:$.channels.coolingTowerMetadata.messages.pumpSpeed.payload",
      "CoolingTowerFanSpeedMetadata": "$ref:$.channels.coolingTowerMetadata.messages.fanSpeed.payload",
      "CoolingTowerDamperPositionMetadata": "$ref:$.channels.coolingTowerMetadata.messages.damperPosition.payload",
      "CoolingTowerAirTemperatureMetadata": "$ref:$.channels.coolingTowerMetadata.messages.airTemperature.payload",
      "CoolingTowerAirDifferentialPressureMetadata": "$ref:$.channels.coolingTowerMetadata.messages.airDifferentialPressure.payload",
      "CoolingTowerAirFlowMetadata": "$ref:$.channels.coolingTowerMetadata.messages.airFlow.payload",
      "CoolingTowerAirPressureMetadata": "$ref:$.channels.coolingTowerMetadata.messages.airPressure.payload",
      "CoolingTowerLeakDetectMetadata": "$ref:$.channels.coolingTowerMetadata.messages.leakDetect.payload",
      "CoolingTowerAirRelativeHumidityMetadata": "$ref:$.channels.coolingTowerMetadata.messages.airRelativeHumidity.payload",
      "HXLiquidTemperatureMetadata": "$ref:$.channels.hxMetadata.messages.liquidTemperature.payload",
      "HXLiquidDifferentialPressureMetadata": "$ref:$.channels.hxMetadata.messages.liquidDifferentialPressure.payload",
      "HXLiquidFlowMetadata": "$ref:$.channels.hxMetadata.messages.liquidFlow.payload",
      "HXLiquidPressureMetadata": "$ref:$.channels.hxMetadata.messages.liquidPressure.payload",
      "HXStatusMetadata": "$ref:$.channels.hxMetadata.messages.status.payload",
      "HXAvailableMetadata": "$ref:$.channels.hxMetadata.messages.available.payload",
      "HXValvePositionMetadata": "$ref:$.channels.hxMetadata.messages.valvePosition.payload",
      "HXPumpSpeedMetadata": "$ref:$.channels.hxMetadata.messages.pumpSpeed.payload",
      "HXFanSpeedMetadata": "$ref:$.channels.hxMetadata.messages.fanSpeed.payload",
      "HXDamperPositionMetadata": "$ref:$.channels.hxMetadata.messages.damperPosition.payload",
      "HXAirTemperatureMetadata": "$ref:$.channels.hxMetadata.messages.airTemperature.payload",
      "HXAirDifferentialPressureMetadata": "$ref:$.channels.hxMetadata.messages.airDifferentialPressure.payload",
      "HXAirFlowMetadata": "$ref:$.channels.hxMetadata.messages.airFlow.payload",
      "HXAirPressureMetadata": "$ref:$.channels.hxMetadata.messages.airPressure.payload",
      "HXLeakDetectMetadata": "$ref:$.channels.hxMetadata.messages.leakDetect.payload",
      "HXAirRelativeHumidityMetadata": "$ref:$.channels.hxMetadata.messages.airRelativeHumidity.payload",
      "CRAHLiquidTemperatureMetadata": "$ref:$.channels.crahMetadata.messages.liquidTemperature.payload",
      "CRAHLiquidDifferentialPressureMetadata": "$ref:$.channels.crahMetadata.messages.liquidDifferentialPressure.payload",
      "CRAHLiquidFlowMetadata": "$ref:$.channels.crahMetadata.messages.liquidFlow.payload",
      "CRAHLiquidPressureMetadata": "$ref:$.channels.crahMetadata.messages.liquidPressure.payload",
      "CRAHStatusMetadata": "$ref:$.channels.crahMetadata.messages.status.payload",
      "CRAHAvailableMetadata": "$ref:$.channels.crahMetadata.messages.available.payload",
      "CRAHValvePositionMetadata": "$ref:$.channels.crahMetadata.messages.valvePosition.payload",
      "CRAHPumpSpeedMetadata": "$ref:$.channels.crahMetadata.messages.pumpSpeed.payload",
      "CRAHFanSpeedMetadata": "$ref:$.channels.crahMetadata.messages.fanSpeed.payload",
      "CRAHDamperPositionMetadata": "$ref:$.channels.crahMetadata.messages.damperPosition.payload",
      "CRAHAirTemperatureMetadata": "$ref:$.channels.crahMetadata.messages.airTemperature.payload",
      "CRAHAirDifferentialPressureMetadata": "$ref:$.channels.crahMetadata.messages.airDifferentialPressure.payload",
      "CRAHAirFlowMetadata": "$ref:$.channels.crahMetadata.messages.airFlow.payload",
      "CRAHAirPressureMetadata": "$ref:$.channels.crahMetadata.messages.airPressure.payload",
      "CRAHLeakDetectMetadata": "$ref:$.channels.crahMetadata.messages.leakDetect.payload",
      "CRAHAirRelativeHumidityMetadata": "$ref:$.channels.crahMetadata.messages.airRelativeHumidity.payload",
      "CRACLiquidTemperatureMetadata": "$ref:$.channels.cracMetadata.messages.liquidTemperature.payload",
      "CRACLiquidDifferentialPressureMetadata": "$ref:$.channels.cracMetadata.messages.liquidDifferentialPressure.payload",
      "CRACLiquidFlowMetadata": "$ref:$.channels.cracMetadata.messages.liquidFlow.payload",
      "CRACLiquidPressureMetadata": "$ref:$.channels.cracMetadata.messages.liquidPressure.payload",
      "CRACStatusMetadata": "$ref:$.channels.cracMetadata.messages.status.payload",
      "CRACAvailableMetadata": "$ref:$.channels.cracMetadata.messages.available.payload",
      "CRACValvePositionMetadata": "$ref:$.channels.cracMetadata.messages.valvePosition.payload",
      "CRACPumpSpeedMetadata": "$ref:$.channels.cracMetadata.messages.pumpSpeed.payload",
      "CRACFanSpeedMetadata": "$ref:$.channels.cracMetadata.messages.fanSpeed.payload",
      "CRACDamperPositionMetadata": "$ref:$.channels.cracMetadata.messages.damperPosition.payload",
      "CRACAirTemperatureMetadata": "$ref:$.channels.cracMetadata.messages.airTemperature.payload",
      "CRACAirDifferentialPressureMetadata": "$ref:$.channels.cracMetadata.messages.airDifferentialPressure.payload",
      "CRACAirFlowMetadata": "$ref:$.channels.cracMetadata.messages.airFlow.payload",
      "CRACAirPressureMetadata": "$ref:$.channels.cracMetadata.messages.airPressure.payload",
      "CRACLeakDetectMetadata": "$ref:$.channels.cracMetadata.messages.leakDetect.payload",
      "CRACAirRelativeHumidityMetadata": "$ref:$.channels.cracMetadata.messages.airRelativeHumidity.payload",
      "AHULiquidTemperatureMetadata": "$ref:$.channels.ahuMetadata.messages.liquidTemperature.payload",
      "AHULiquidDifferentialPressureMetadata": "$ref:$.channels.ahuMetadata.messages.liquidDifferentialPressure.payload",
      "AHULiquidFlowMetadata": "$ref:$.channels.ahuMetadata.messages.liquidFlow.payload",
      "AHULiquidPressureMetadata": "$ref:$.channels.ahuMetadata.messages.liquidPressure.payload",
      "AHUStatusMetadata": "$ref:$.channels.ahuMetadata.messages.status.payload",
      "AHUAvailableMetadata": "$ref:$.channels.ahuMetadata.messages.available.payload",
      "AHUValvePositionMetadata": "$ref:$.channels.ahuMetadata.messages.valvePosition.payload",
      "AHUPumpSpeedMetadata": "$ref:$.channels.ahuMetadata.messages.pumpSpeed.payload",
      "AHUFanSpeedMetadata": "$ref:$.channels.ahuMetadata.messages.fanSpeed.payload",
      "AHUDamperPositionMetadata": "$ref:$.channels.ahuMetadata.messages.damperPosition.payload",
      "AHUAirTemperatureMetadata": "$ref:$.channels.ahuMetadata.messages.airTemperature.payload",
      "AHUAirDifferentialPressureMetadata": "$ref:$.channels.ahuMetadata.messages.airDifferentialPressure.payload",
      "AHUAirFlowMetadata": "$ref:$.channels.ahuMetadata.messages.airFlow.payload",
      "AHUAirPressureMetadata": "$ref:$.channels.ahuMetadata.messages.airPressure.payload",
      "AHULeakDetectMetadata": "$ref:$.channels.ahuMetadata.messages.leakDetect.payload",
      "AHUAirRelativeHumidityMetadata": "$ref:$.channels.ahuMetadata.messages.airRelativeHumidity.payload",
      "ChillerLiquidTemperatureMetadata": "$ref:$.channels.chillerMetadata.messages.liquidTemperature.payload",
      "ChillerLiquidDifferentialPressureMetadata": "$ref:$.channels.chillerMetadata.messages.liquidDifferentialPressure.payload",
      "ChillerLiquidFlowMetadata": "$ref:$.channels.chillerMetadata.messages.liquidFlow.payload",
      "ChillerLiquidPressureMetadata": "$ref:$.channels.chillerMetadata.messages.liquidPressure.payload",
      "ChillerStatusMetadata": "$ref:$.channels.chillerMetadata.messages.status.payload",
      "ChillerAvailableMetadata": "$ref:$.channels.chillerMetadata.messages.available.payload",
      "ChillerValvePositionMetadata": "$ref:$.channels.chillerMetadata.messages.valvePosition.payload",
      "ChillerPumpSpeedMetadata": "$ref:$.channels.chillerMetadata.messages.pumpSpeed.payload",
      "ChillerFanSpeedMetadata": "$ref:$.channels.chillerMetadata.messages.fanSpeed.payload",
      "ChillerDamperPositionMetadata": "$ref:$.channels.chillerMetadata.messages.damperPosition.payload",
      "ChillerAirTemperatureMetadata": "$ref:$.channels.chillerMetadata.messages.airTemperature.payload",
      "ChillerAirDifferentialPressureMetadata": "$ref:$.channels.chillerMetadata.messages.airDifferentialPressure.payload",
      "ChillerAirFlowMetadata": "$ref:$.channels.chillerMetadata.messages.airFlow.payload",
      "ChillerAirPressureMetadata": "$ref:$.channels.chillerMetadata.messages.airPressure.payload",
      "ChillerLeakDetectMetadata": "$ref:$.channels.chillerMetadata.messages.leakDetect.payload",
      "ChillerAirRelativeHumidityMetadata": "$ref:$.channels.chillerMetadata.messages.airRelativeHumidity.payload",
      "TankLiquidTemperatureMetadata": "$ref:$.channels.tankMetadata.messages.liquidTemperature.payload",
      "TankLiquidDifferentialPressureMetadata": "$ref:$.channels.tankMetadata.messages.liquidDifferentialPressure.payload",
      "TankLiquidFlowMetadata": "$ref:$.channels.tankMetadata.messages.liquidFlow.payload",
      "TankLiquidPressureMetadata": "$ref:$.channels.tankMetadata.messages.liquidPressure.payload",
      "TankStatusMetadata": "$ref:$.channels.tankMetadata.messages.status.payload",
      "TankAvailableMetadata": "$ref:$.channels.tankMetadata.messages.available.payload",
      "TankValvePositionMetadata": "$ref:$.channels.tankMetadata.messages.valvePosition.payload",
      "TankPumpSpeedMetadata": "$ref:$.channels.tankMetadata.messages.pumpSpeed.payload",
      "TankFanSpeedMetadata": "$ref:$.channels.tankMetadata.messages.fanSpeed.payload",
      "TankDamperPositionMetadata": "$ref:$.channels.tankMetadata.messages.damperPosition.payload",
      "TankAirTemperatureMetadata": "$ref:$.channels.tankMetadata.messages.airTemperature.payload",
      "TankAirDifferentialPressureMetadata": "$ref:$.channels.tankMetadata.messages.airDifferentialPressure.payload",
      "TankAirFlowMetadata": "$ref:$.channels.tankMetadata.messages.airFlow.payload",
      "TankAirPressureMetadata": "$ref:$.channels.tankMetadata.messages.airPressure.payload",
      "TankLeakDetectMetadata": "$ref:$.channels.tankMetadata.messages.leakDetect.payload",
      "TankAirRelativeHumidityMetadata": "$ref:$.channels.tankMetadata.messages.airRelativeHumidity.payload",
      "GenericObjectLiquidTemperatureMetadata": "$ref:$.channels.genericObjectMetadata.messages.liquidTemperature.payload",
      "GenericObjectLiquidDifferentialPressureMetadata": "$ref:$.channels.genericObjectMetadata.messages.liquidDifferentialPressure.payload",
      "GenericObjectLiquidFlowMetadata": "$ref:$.channels.genericObjectMetadata.messages.liquidFlow.payload",
      "GenericObjectLiquidPressureMetadata": "$ref:$.channels.genericObjectMetadata.messages.liquidPressure.payload",
      "GenericObjectStatusMetadata": "$ref:$.channels.genericObjectMetadata.messages.status.payload",
      "GenericObjectAvailableMetadata": "$ref:$.channels.genericObjectMetadata.messages.available.payload",
      "GenericObjectValvePositionMetadata": "$ref:$.channels.genericObjectMetadata.messages.valvePosition.payload",
      "GenericObjectPumpSpeedMetadata": "$ref:$.channels.genericObjectMetadata.messages.pumpSpeed.payload",
      "GenericObjectFanSpeedMetadata": "$ref:$.channels.genericObjectMetadata.messages.fanSpeed.payload",
      "GenericObjectDamperPositionMetadata": "$ref:$.channels.genericObjectMetadata.messages.damperPosition.payload",
      "GenericObjectAirTemperatureMetadata": "$ref:$.channels.genericObjectMetadata.messages.airTemperature.payload",
      "GenericObjectAirDifferentialPressureMetadata": "$ref:$.channels.genericObjectMetadata.messages.airDifferentialPressure.payload",
      "GenericObjectAirFlowMetadata": "$ref:$.channels.genericObjectMetadata.messages.airFlow.payload",
      "GenericObjectAirPressureMetadata": "$ref:$.channels.genericObjectMetadata.messages.airPressure.payload",
      "GenericObjectSoundMetadata": "$ref:$.channels.genericObjectMetadata.messages.sound.payload",
      "GenericObjectLeakDetectMetadata": "$ref:$.channels.genericObjectMetadata.messages.leakDetect.payload",
      "GenericObjectAirRelativeHumidityMetadata": "$ref:$.channels.genericObjectMetadata.messages.airRelativeHumidity.payload",
      "GenericObjectLiquidTemperatureSpRequestMetadata": "$ref:$.channels.genericObjectMetadata.messages.liquidTemperatureSpRequest.payload",
      "GenericPointFields": "$ref:$.channels.powerMeterMetadata.messages.genericPoint.payload.allOf[1]",
      "GenericEquipmentPointMetadata": "$ref:$.channels.bessMetadata.messages.genericPoint.payload",
      "GenericPowerMeterPointMetadata": "$ref:$.channels.powerMeterMetadata.messages.genericPoint.payload"
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
  