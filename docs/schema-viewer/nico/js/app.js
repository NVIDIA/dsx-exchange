
    const schema = {
  "asyncapi": "3.1.0",
  "info": {
    "title": "NICo Managed Host State Event Bus",
    "version": "1.0.0",
    "description": "AsyncAPI specification for NICo Managed Host State change events. This spec defines the message structure for publishing managed host state transitions and related metadata including DPU states, host initialization, validation, assignment, and reprovisioning states.\nThe MQTT topic path should follow the following format:\n- **State Changes**: nico/v1/machine/{machineId}/state\n"
  },
  "servers": {
    "production": {
      "host": "broker.example.com",
      "protocol": "mqtt",
      "description": "MQTT broker for NICo managed host state events"
    }
  },
  "channels": {
    "managedHostState": {
      "address": "nico/v1/machine/{machineId}/state",
      "parameters": {
        "machineId": {
          "description": "Unique identifier for the managed host machine."
        }
      },
      "messages": {
        "stateChangeMessage": {
          "name": "ManagedHostStateChangeMessage",
          "title": "Managed Host State Change",
          "payload": {
            "type": "object",
            "required": [
              "machine_id",
              "managed_host_state",
              "timestamp"
            ],
            "properties": {
              "machine_id": {
                "type": "string",
                "description": "Unique identifier for the managed host machine.",
                "x-parser-schema-id": "<anonymous-schema-3>"
              },
              "timestamp": {
                "type": "string",
                "format": "date-time",
                "description": "ISO 8601 timestamp of the state change.",
                "x-parser-schema-id": "<anonymous-schema-4>"
              },
              "managed_host_state": {
                "description": "Possible Machine state-machine implementation Possible ManagedHost state-machine implementation Only DPU machine field in DB will contain state. Host will be empty. DPU state field will be used to derive state for DPU and Host both.",
                "oneOf": [
                  {
                    "description": "Dpu was discovered by a site-explorer and is being configuring via redfish.",
                    "type": "object",
                    "required": [
                      "dpu_states",
                      "state"
                    ],
                    "properties": {
                      "dpu_states": {
                        "type": "object",
                        "required": [
                          "states"
                        ],
                        "properties": {
                          "states": {
                            "type": "object",
                            "additionalProperties": {
                              "oneOf": [
                                {
                                  "description": "Dpu discovery via redfish states",
                                  "type": "object",
                                  "required": [
                                    "dpudiscoverystate"
                                  ],
                                  "properties": {
                                    "dpudiscoverystate": {
                                      "type": "string",
                                      "enum": [
                                        "initializing"
                                      ],
                                      "x-parser-schema-id": "<anonymous-schema-8>"
                                    }
                                  },
                                  "x-parser-schema-id": "<anonymous-schema-7>"
                                },
                                {
                                  "type": "object",
                                  "required": [
                                    "dpudiscoverystate"
                                  ],
                                  "properties": {
                                    "dpudiscoverystate": {
                                      "type": "string",
                                      "enum": [
                                        "configuring"
                                      ],
                                      "x-parser-schema-id": "<anonymous-schema-10>"
                                    }
                                  },
                                  "x-parser-schema-id": "<anonymous-schema-9>"
                                },
                                {
                                  "type": "object",
                                  "required": [
                                    "dpudiscoverystate"
                                  ],
                                  "properties": {
                                    "dpudiscoverystate": {
                                      "type": "string",
                                      "enum": [
                                        "rebootalldpus"
                                      ],
                                      "x-parser-schema-id": "<anonymous-schema-12>"
                                    }
                                  },
                                  "x-parser-schema-id": "<anonymous-schema-11>"
                                },
                                {
                                  "type": "object",
                                  "required": [
                                    "count",
                                    "dpudiscoverystate",
                                    "enable_secure_boot_state"
                                  ],
                                  "properties": {
                                    "count": {
                                      "type": "integer",
                                      "format": "uint32",
                                      "minimum": 0,
                                      "x-parser-schema-id": "<anonymous-schema-14>"
                                    },
                                    "dpudiscoverystate": {
                                      "type": "string",
                                      "enum": [
                                        "enablesecureboot"
                                      ],
                                      "x-parser-schema-id": "<anonymous-schema-15>"
                                    },
                                    "enable_secure_boot_state": {
                                      "oneOf": [
                                        {
                                          "type": "object",
                                          "required": [
                                            "disablesecurebootstate"
                                          ],
                                          "properties": {
                                            "disablesecurebootstate": {
                                              "type": "string",
                                              "enum": [
                                                "checksecurebootstatus"
                                              ],
                                              "x-parser-schema-id": "<anonymous-schema-17>"
                                            }
                                          },
                                          "x-parser-schema-id": "<anonymous-schema-16>"
                                        },
                                        {
                                          "type": "object",
                                          "required": [
                                            "disablesecurebootstate"
                                          ],
                                          "properties": {
                                            "disablesecurebootstate": {
                                              "type": "string",
                                              "enum": [
                                                "disablesecureboot"
                                              ],
                                              "x-parser-schema-id": "<anonymous-schema-19>"
                                            }
                                          },
                                          "x-parser-schema-id": "<anonymous-schema-18>"
                                        },
                                        {
                                          "type": "object",
                                          "required": [
                                            "disablesecurebootstate"
                                          ],
                                          "properties": {
                                            "disablesecurebootstate": {
                                              "type": "string",
                                              "enum": [
                                                "setsecureboot"
                                              ],
                                              "x-parser-schema-id": "<anonymous-schema-21>"
                                            }
                                          },
                                          "x-parser-schema-id": "<anonymous-schema-20>"
                                        },
                                        {
                                          "type": "object",
                                          "required": [
                                            "disablesecurebootstate",
                                            "reboot_count"
                                          ],
                                          "properties": {
                                            "disablesecurebootstate": {
                                              "type": "string",
                                              "enum": [
                                                "rebootdpu"
                                              ],
                                              "x-parser-schema-id": "<anonymous-schema-23>"
                                            },
                                            "reboot_count": {
                                              "type": "integer",
                                              "format": "uint32",
                                              "minimum": 0,
                                              "x-parser-schema-id": "<anonymous-schema-24>"
                                            }
                                          },
                                          "x-parser-schema-id": "<anonymous-schema-22>"
                                        },
                                        {
                                          "type": "object",
                                          "required": [
                                            "disablesecurebootstate",
                                            "task_id"
                                          ],
                                          "properties": {
                                            "disablesecurebootstate": {
                                              "type": "string",
                                              "enum": [
                                                "waitcertificateupload"
                                              ],
                                              "x-parser-schema-id": "<anonymous-schema-26>"
                                            },
                                            "task_id": {
                                              "type": "string",
                                              "x-parser-schema-id": "<anonymous-schema-27>"
                                            }
                                          },
                                          "x-parser-schema-id": "<anonymous-schema-25>"
                                        }
                                      ],
                                      "x-parser-schema-id": "SetSecureBootState"
                                    }
                                  },
                                  "x-parser-schema-id": "<anonymous-schema-13>"
                                },
                                {
                                  "type": "object",
                                  "required": [
                                    "count",
                                    "dpudiscoverystate"
                                  ],
                                  "properties": {
                                    "count": {
                                      "type": "integer",
                                      "format": "uint32",
                                      "minimum": 0,
                                      "x-parser-schema-id": "<anonymous-schema-29>"
                                    },
                                    "disable_secure_boot_state": {
                                      "anyOf": [
                                        "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[0].properties.dpu_states.properties.states.additionalProperties.oneOf[3].properties.enable_secure_boot_state",
                                        {
                                          "type": "null",
                                          "x-parser-schema-id": "<anonymous-schema-31>"
                                        }
                                      ],
                                      "x-parser-schema-id": "<anonymous-schema-30>"
                                    },
                                    "dpudiscoverystate": {
                                      "type": "string",
                                      "enum": [
                                        "disablesecureboot"
                                      ],
                                      "x-parser-schema-id": "<anonymous-schema-32>"
                                    }
                                  },
                                  "x-parser-schema-id": "<anonymous-schema-28>"
                                },
                                {
                                  "type": "object",
                                  "required": [
                                    "dpudiscoverystate"
                                  ],
                                  "properties": {
                                    "dpudiscoverystate": {
                                      "type": "string",
                                      "enum": [
                                        "setuefihttpboot"
                                      ],
                                      "x-parser-schema-id": "<anonymous-schema-34>"
                                    }
                                  },
                                  "x-parser-schema-id": "<anonymous-schema-33>"
                                },
                                {
                                  "type": "object",
                                  "required": [
                                    "dpudiscoverystate"
                                  ],
                                  "properties": {
                                    "dpudiscoverystate": {
                                      "type": "string",
                                      "enum": [
                                        "enablershim"
                                      ],
                                      "x-parser-schema-id": "<anonymous-schema-36>"
                                    }
                                  },
                                  "x-parser-schema-id": "<anonymous-schema-35>"
                                }
                              ],
                              "x-parser-schema-id": "DpuDiscoveringState"
                            },
                            "x-parser-schema-id": "<anonymous-schema-6>"
                          }
                        },
                        "x-parser-schema-id": "DpuDiscoveringStates"
                      },
                      "state": {
                        "type": "string",
                        "enum": [
                          "dpudiscoveringstate"
                        ],
                        "x-parser-schema-id": "<anonymous-schema-37>"
                      }
                    },
                    "x-parser-schema-id": "<anonymous-schema-5>"
                  },
                  {
                    "description": "DPU is not yet ready.",
                    "type": "object",
                    "required": [
                      "dpu_states",
                      "state"
                    ],
                    "properties": {
                      "dpu_states": {
                        "type": "object",
                        "required": [
                          "states"
                        ],
                        "properties": {
                          "states": {
                            "type": "object",
                            "additionalProperties": {
                              "oneOf": [
                                {
                                  "type": "object",
                                  "required": [
                                    "dpustate",
                                    "substate"
                                  ],
                                  "properties": {
                                    "dpustate": {
                                      "type": "string",
                                      "enum": [
                                        "installdpuos"
                                      ],
                                      "x-parser-schema-id": "<anonymous-schema-41>"
                                    },
                                    "substate": {
                                      "oneOf": [
                                        {
                                          "type": "object",
                                          "required": [
                                            "installdpuosstate"
                                          ],
                                          "properties": {
                                            "installdpuosstate": {
                                              "type": "string",
                                              "enum": [
                                                "installingbfb"
                                              ],
                                              "x-parser-schema-id": "<anonymous-schema-43>"
                                            }
                                          },
                                          "x-parser-schema-id": "<anonymous-schema-42>"
                                        },
                                        {
                                          "type": "object",
                                          "required": [
                                            "installdpuosstate",
                                            "progress",
                                            "task_id"
                                          ],
                                          "properties": {
                                            "installdpuosstate": {
                                              "type": "string",
                                              "enum": [
                                                "waitforinstallcomplete"
                                              ],
                                              "x-parser-schema-id": "<anonymous-schema-45>"
                                            },
                                            "progress": {
                                              "type": "string",
                                              "x-parser-schema-id": "<anonymous-schema-46>"
                                            },
                                            "task_id": {
                                              "type": "string",
                                              "x-parser-schema-id": "<anonymous-schema-47>"
                                            }
                                          },
                                          "x-parser-schema-id": "<anonymous-schema-44>"
                                        },
                                        {
                                          "type": "object",
                                          "required": [
                                            "installdpuosstate"
                                          ],
                                          "properties": {
                                            "installdpuosstate": {
                                              "type": "string",
                                              "enum": [
                                                "completed"
                                              ],
                                              "x-parser-schema-id": "<anonymous-schema-49>"
                                            }
                                          },
                                          "x-parser-schema-id": "<anonymous-schema-48>"
                                        },
                                        {
                                          "type": "object",
                                          "required": [
                                            "installdpuosstate",
                                            "msg"
                                          ],
                                          "properties": {
                                            "installdpuosstate": {
                                              "type": "string",
                                              "enum": [
                                                "installationerror"
                                              ],
                                              "x-parser-schema-id": "<anonymous-schema-51>"
                                            },
                                            "msg": {
                                              "type": "string",
                                              "x-parser-schema-id": "<anonymous-schema-52>"
                                            }
                                          },
                                          "x-parser-schema-id": "<anonymous-schema-50>"
                                        }
                                      ],
                                      "x-parser-schema-id": "InstallDpuOsState"
                                    }
                                  },
                                  "x-parser-schema-id": "<anonymous-schema-40>"
                                },
                                {
                                  "type": "object",
                                  "required": [
                                    "dpustate"
                                  ],
                                  "properties": {
                                    "dpustate": {
                                      "type": "string",
                                      "enum": [
                                        "init"
                                      ],
                                      "x-parser-schema-id": "<anonymous-schema-54>"
                                    }
                                  },
                                  "x-parser-schema-id": "<anonymous-schema-53>"
                                },
                                {
                                  "type": "object",
                                  "required": [
                                    "dpustate",
                                    "substate"
                                  ],
                                  "properties": {
                                    "dpustate": {
                                      "type": "string",
                                      "enum": [
                                        "waitingforplatformpowercycle"
                                      ],
                                      "x-parser-schema-id": "<anonymous-schema-56>"
                                    },
                                    "substate": {
                                      "oneOf": [
                                        {
                                          "type": "object",
                                          "required": [
                                            "state"
                                          ],
                                          "properties": {
                                            "state": {
                                              "type": "string",
                                              "enum": [
                                                "off"
                                              ],
                                              "x-parser-schema-id": "<anonymous-schema-58>"
                                            }
                                          },
                                          "x-parser-schema-id": "<anonymous-schema-57>"
                                        },
                                        {
                                          "type": "object",
                                          "required": [
                                            "state"
                                          ],
                                          "properties": {
                                            "state": {
                                              "type": "string",
                                              "enum": [
                                                "on"
                                              ],
                                              "x-parser-schema-id": "<anonymous-schema-60>"
                                            }
                                          },
                                          "x-parser-schema-id": "<anonymous-schema-59>"
                                        }
                                      ],
                                      "x-parser-schema-id": "PerformPowerOperation"
                                    }
                                  },
                                  "x-parser-schema-id": "<anonymous-schema-55>"
                                },
                                {
                                  "type": "object",
                                  "required": [
                                    "dpustate"
                                  ],
                                  "properties": {
                                    "dpustate": {
                                      "type": "string",
                                      "enum": [
                                        "waitingforplatformconfiguration"
                                      ],
                                      "x-parser-schema-id": "<anonymous-schema-62>"
                                    }
                                  },
                                  "x-parser-schema-id": "<anonymous-schema-61>"
                                },
                                {
                                  "type": "object",
                                  "required": [
                                    "dpustate"
                                  ],
                                  "properties": {
                                    "dpustate": {
                                      "type": "string",
                                      "enum": [
                                        "pollingbiossetup"
                                      ],
                                      "x-parser-schema-id": "<anonymous-schema-64>"
                                    }
                                  },
                                  "x-parser-schema-id": "<anonymous-schema-63>"
                                },
                                {
                                  "type": "object",
                                  "required": [
                                    "dpustate"
                                  ],
                                  "properties": {
                                    "dpustate": {
                                      "type": "string",
                                      "enum": [
                                        "waitingfornetworkconfig"
                                      ],
                                      "x-parser-schema-id": "<anonymous-schema-66>"
                                    }
                                  },
                                  "x-parser-schema-id": "<anonymous-schema-65>"
                                },
                                {
                                  "type": "object",
                                  "required": [
                                    "dpustate"
                                  ],
                                  "properties": {
                                    "dpustate": {
                                      "type": "string",
                                      "enum": [
                                        "waitingfornetworkinstall"
                                      ],
                                      "x-parser-schema-id": "<anonymous-schema-68>"
                                    }
                                  },
                                  "x-parser-schema-id": "<anonymous-schema-67>"
                                }
                              ],
                              "x-parser-schema-id": "DpuInitState"
                            },
                            "x-parser-schema-id": "<anonymous-schema-39>"
                          }
                        },
                        "x-parser-schema-id": "DpuInitStates"
                      },
                      "state": {
                        "type": "string",
                        "enum": [
                          "dpuinit"
                        ],
                        "x-parser-schema-id": "<anonymous-schema-69>"
                      }
                    },
                    "x-parser-schema-id": "<anonymous-schema-38>"
                  },
                  {
                    "description": "DPU is ready, Host is not yet Ready.",
                    "type": "object",
                    "required": [
                      "machine_state",
                      "state"
                    ],
                    "properties": {
                      "machine_state": {
                        "oneOf": [
                          {
                            "type": "object",
                            "required": [
                              "state"
                            ],
                            "properties": {
                              "state": {
                                "type": "string",
                                "enum": [
                                  "init"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-72>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-71>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "state"
                            ],
                            "properties": {
                              "state": {
                                "type": "string",
                                "enum": [
                                  "enableipmioverlan"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-74>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-73>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "state"
                            ],
                            "properties": {
                              "state": {
                                "type": "string",
                                "enum": [
                                  "waitingforplatformconfiguration"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-76>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-75>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "state"
                            ],
                            "properties": {
                              "state": {
                                "type": "string",
                                "enum": [
                                  "pollingbiossetup"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-78>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-77>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "state"
                            ],
                            "properties": {
                              "set_boot_order_info": {
                                "anyOf": [
                                  {
                                    "type": "object",
                                    "required": [
                                      "set_boot_order_state"
                                    ],
                                    "properties": {
                                      "set_boot_order_jid": {
                                        "type": [
                                          "string",
                                          "null"
                                        ],
                                        "x-parser-schema-id": "<anonymous-schema-81>"
                                      },
                                      "set_boot_order_state": {
                                        "oneOf": [
                                          {
                                            "type": "object",
                                            "required": [
                                              "state"
                                            ],
                                            "properties": {
                                              "state": {
                                                "type": "string",
                                                "enum": [
                                                  "setbootorder"
                                                ],
                                                "x-parser-schema-id": "<anonymous-schema-83>"
                                              }
                                            },
                                            "x-parser-schema-id": "<anonymous-schema-82>"
                                          },
                                          {
                                            "type": "object",
                                            "required": [
                                              "state"
                                            ],
                                            "properties": {
                                              "state": {
                                                "type": "string",
                                                "enum": [
                                                  "waitforsetbootorderjobscheduled"
                                                ],
                                                "x-parser-schema-id": "<anonymous-schema-85>"
                                              }
                                            },
                                            "x-parser-schema-id": "<anonymous-schema-84>"
                                          },
                                          {
                                            "type": "object",
                                            "required": [
                                              "state"
                                            ],
                                            "properties": {
                                              "state": {
                                                "type": "string",
                                                "enum": [
                                                  "reboothost"
                                                ],
                                                "x-parser-schema-id": "<anonymous-schema-87>"
                                              }
                                            },
                                            "x-parser-schema-id": "<anonymous-schema-86>"
                                          },
                                          {
                                            "type": "object",
                                            "required": [
                                              "state"
                                            ],
                                            "properties": {
                                              "state": {
                                                "type": "string",
                                                "enum": [
                                                  "waitforsetbootorderjobcompletion"
                                                ],
                                                "x-parser-schema-id": "<anonymous-schema-89>"
                                              }
                                            },
                                            "x-parser-schema-id": "<anonymous-schema-88>"
                                          }
                                        ],
                                        "x-parser-schema-id": "SetBootOrderState"
                                      }
                                    },
                                    "x-parser-schema-id": "SetBootOrderInfo"
                                  },
                                  {
                                    "type": "null",
                                    "x-parser-schema-id": "<anonymous-schema-90>"
                                  }
                                ],
                                "x-parser-schema-id": "<anonymous-schema-80>"
                              },
                              "state": {
                                "type": "string",
                                "enum": [
                                  "setbootorder"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-91>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-79>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "state",
                              "uefi_setup_info"
                            ],
                            "properties": {
                              "state": {
                                "type": "string",
                                "enum": [
                                  "uefisetup"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-93>"
                              },
                              "uefi_setup_info": {
                                "type": "object",
                                "required": [
                                  "uefi_setup_state"
                                ],
                                "properties": {
                                  "uefi_password_jid": {
                                    "type": [
                                      "string",
                                      "null"
                                    ],
                                    "x-parser-schema-id": "<anonymous-schema-94>"
                                  },
                                  "uefi_setup_state": {
                                    "description": "Substates of enabling/disabling lockdown",
                                    "oneOf": [
                                      {
                                        "type": "object",
                                        "required": [
                                          "state"
                                        ],
                                        "properties": {
                                          "state": {
                                            "type": "string",
                                            "enum": [
                                              "unlockhost"
                                            ],
                                            "x-parser-schema-id": "<anonymous-schema-96>"
                                          }
                                        },
                                        "x-parser-schema-id": "<anonymous-schema-95>"
                                      },
                                      {
                                        "type": "object",
                                        "required": [
                                          "state"
                                        ],
                                        "properties": {
                                          "state": {
                                            "type": "string",
                                            "enum": [
                                              "setuefipassword"
                                            ],
                                            "x-parser-schema-id": "<anonymous-schema-98>"
                                          }
                                        },
                                        "x-parser-schema-id": "<anonymous-schema-97>"
                                      },
                                      {
                                        "type": "object",
                                        "required": [
                                          "state"
                                        ],
                                        "properties": {
                                          "state": {
                                            "type": "string",
                                            "enum": [
                                              "waitforpasswordjobscheduled"
                                            ],
                                            "x-parser-schema-id": "<anonymous-schema-100>"
                                          }
                                        },
                                        "x-parser-schema-id": "<anonymous-schema-99>"
                                      },
                                      {
                                        "type": "object",
                                        "required": [
                                          "state"
                                        ],
                                        "properties": {
                                          "state": {
                                            "type": "string",
                                            "enum": [
                                              "powercyclehost"
                                            ],
                                            "x-parser-schema-id": "<anonymous-schema-102>"
                                          }
                                        },
                                        "x-parser-schema-id": "<anonymous-schema-101>"
                                      },
                                      {
                                        "type": "object",
                                        "required": [
                                          "state"
                                        ],
                                        "properties": {
                                          "state": {
                                            "type": "string",
                                            "enum": [
                                              "waitforpasswordjobcompletion"
                                            ],
                                            "x-parser-schema-id": "<anonymous-schema-104>"
                                          }
                                        },
                                        "x-parser-schema-id": "<anonymous-schema-103>"
                                      },
                                      {
                                        "type": "object",
                                        "required": [
                                          "state"
                                        ],
                                        "properties": {
                                          "state": {
                                            "type": "string",
                                            "enum": [
                                              "lockdownhost"
                                            ],
                                            "x-parser-schema-id": "<anonymous-schema-106>"
                                          }
                                        },
                                        "x-parser-schema-id": "<anonymous-schema-105>"
                                      }
                                    ],
                                    "x-parser-schema-id": "UefiSetupState"
                                  }
                                },
                                "x-parser-schema-id": "UefiSetupInfo"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-92>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "measuring_state",
                              "state"
                            ],
                            "properties": {
                              "measuring_state": {
                                "description": "MeasuringState contains states used for host attestion (or measured boot).",
                                "oneOf": [
                                  {
                                    "description": "WaitingForMeasurements is reported when the machine has reached a state where the API is now expecting measurements from the machine, which Scout sends upon receiving an Action::Measure from the API.",
                                    "type": "string",
                                    "enum": [
                                      "waitingformeasurements"
                                    ],
                                    "x-parser-schema-id": "<anonymous-schema-108>"
                                  },
                                  {
                                    "description": "PendingBundle is reported when the API has received measurements from the machine, but the measurements do not match a known bundle. At this point, a matching bundle needs to be created, either via \"promoting\" a measurement report from a machine (through manual interaction or trusted approval automation), or by manually creating a new bundle.",
                                    "type": "string",
                                    "enum": [
                                      "pendingbundle"
                                    ],
                                    "x-parser-schema-id": "<anonymous-schema-109>"
                                  }
                                ],
                                "x-parser-schema-id": "MeasuringState"
                              },
                              "state": {
                                "type": "string",
                                "enum": [
                                  "measuring"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-110>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-107>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "state"
                            ],
                            "properties": {
                              "state": {
                                "type": "string",
                                "enum": [
                                  "waitingfordiscovery"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-112>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-111>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "state"
                            ],
                            "properties": {
                              "skip_reboot_wait": {
                                "default": false,
                                "type": "boolean",
                                "x-parser-schema-id": "<anonymous-schema-114>"
                              },
                              "state": {
                                "type": "string",
                                "enum": [
                                  "discovered"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-115>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-113>"
                          },
                          {
                            "description": "Lockdown handling.",
                            "type": "object",
                            "required": [
                              "lockdown_info",
                              "state"
                            ],
                            "properties": {
                              "lockdown_info": {
                                "type": "object",
                                "required": [
                                  "mode",
                                  "state"
                                ],
                                "properties": {
                                  "mode": {
                                    "description": "Whether lockdown should be enabled or disabled in an operation",
                                    "type": "string",
                                    "enum": [
                                      "enable",
                                      "disable"
                                    ],
                                    "x-parser-schema-id": "LockdownMode"
                                  },
                                  "state": {
                                    "type": "string",
                                    "enum": [
                                      "setlockdown",
                                      "timewaitfordpudown",
                                      "waitfordpuup",
                                      "pollinglockdownstatus"
                                    ],
                                    "x-parser-schema-id": "LockdownState"
                                  }
                                },
                                "x-parser-schema-id": "LockdownInfo"
                              },
                              "state": {
                                "type": "string",
                                "enum": [
                                  "waitingforlockdown"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-117>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-116>"
                          }
                        ],
                        "x-parser-schema-id": "MachineState"
                      },
                      "state": {
                        "type": "string",
                        "enum": [
                          "hostinit"
                        ],
                        "x-parser-schema-id": "<anonymous-schema-118>"
                      }
                    },
                    "x-parser-schema-id": "<anonymous-schema-70>"
                  },
                  {
                    "description": "Host validation state for machine and DPU validation",
                    "type": "object",
                    "required": [
                      "state",
                      "validation_state"
                    ],
                    "properties": {
                      "state": {
                        "type": "string",
                        "enum": [
                          "validation"
                        ],
                        "x-parser-schema-id": "<anonymous-schema-120>"
                      },
                      "validation_state": {
                        "oneOf": [
                          {
                            "description": "Host machine validation placeholder for DPU machine validation TODO: add DPU validation state SKU validatioon can also be moved here, so that all validation done @ one place",
                            "type": "object",
                            "required": [
                              "machine_validation",
                              "validation_type"
                            ],
                            "properties": {
                              "machine_validation": {
                                "oneOf": [
                                  {
                                    "type": "object",
                                    "required": [
                                      "reboothost"
                                    ],
                                    "properties": {
                                      "reboothost": {
                                        "type": "object",
                                        "required": [
                                          "validation_id"
                                        ],
                                        "properties": {
                                          "validation_id": {
                                            "type": "string",
                                            "format": "uuid",
                                            "x-parser-schema-id": "<anonymous-schema-124>"
                                          }
                                        },
                                        "x-parser-schema-id": "<anonymous-schema-123>"
                                      }
                                    },
                                    "additionalProperties": false,
                                    "x-parser-schema-id": "<anonymous-schema-122>"
                                  },
                                  {
                                    "type": "object",
                                    "required": [
                                      "machinevalidating"
                                    ],
                                    "properties": {
                                      "machinevalidating": {
                                        "type": "object",
                                        "required": [
                                          "completed",
                                          "context",
                                          "id",
                                          "total"
                                        ],
                                        "properties": {
                                          "completed": {
                                            "type": "integer",
                                            "format": "uint",
                                            "minimum": 0,
                                            "x-parser-schema-id": "<anonymous-schema-127>"
                                          },
                                          "context": {
                                            "type": "string",
                                            "x-parser-schema-id": "<anonymous-schema-128>"
                                          },
                                          "id": {
                                            "type": "string",
                                            "format": "uuid",
                                            "x-parser-schema-id": "<anonymous-schema-129>"
                                          },
                                          "is_enabled": {
                                            "default": true,
                                            "type": "boolean",
                                            "x-parser-schema-id": "<anonymous-schema-130>"
                                          },
                                          "total": {
                                            "type": "integer",
                                            "format": "uint",
                                            "minimum": 0,
                                            "x-parser-schema-id": "<anonymous-schema-131>"
                                          }
                                        },
                                        "x-parser-schema-id": "<anonymous-schema-126>"
                                      }
                                    },
                                    "additionalProperties": false,
                                    "x-parser-schema-id": "<anonymous-schema-125>"
                                  }
                                ],
                                "x-parser-schema-id": "MachineValidatingState"
                              },
                              "validation_type": {
                                "type": "string",
                                "enum": [
                                  "machinevalidation"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-132>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-121>"
                          }
                        ],
                        "x-parser-schema-id": "ValidationState"
                      }
                    },
                    "x-parser-schema-id": "<anonymous-schema-119>"
                  },
                  {
                    "description": "Host is Ready for instance creation.",
                    "type": "object",
                    "required": [
                      "state"
                    ],
                    "properties": {
                      "state": {
                        "type": "string",
                        "enum": [
                          "ready"
                        ],
                        "x-parser-schema-id": "<anonymous-schema-134>"
                      }
                    },
                    "x-parser-schema-id": "<anonymous-schema-133>"
                  },
                  {
                    "description": "Host is assigned to an Instance.",
                    "type": "object",
                    "required": [
                      "instance_state",
                      "state"
                    ],
                    "properties": {
                      "instance_state": {
                        "description": "Possible Instance state-machine implementation, for when the machine host is assigned to a tenant",
                        "oneOf": [
                          {
                            "type": "object",
                            "required": [
                              "state"
                            ],
                            "properties": {
                              "state": {
                                "type": "string",
                                "enum": [
                                  "init"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-137>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-136>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "state"
                            ],
                            "properties": {
                              "state": {
                                "type": "string",
                                "enum": [
                                  "waitingfornetworksegmenttobeready"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-139>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-138>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "state"
                            ],
                            "properties": {
                              "state": {
                                "type": "string",
                                "enum": [
                                  "waitingfornetworkconfig"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-141>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-140>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "state"
                            ],
                            "properties": {
                              "state": {
                                "type": "string",
                                "enum": [
                                  "waitingforstorageconfig"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-143>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-142>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "state"
                            ],
                            "properties": {
                              "state": {
                                "type": "string",
                                "enum": [
                                  "dpaprovisioning"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-145>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-144>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "state"
                            ],
                            "properties": {
                              "state": {
                                "type": "string",
                                "enum": [
                                  "waitingfordpatobeready"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-147>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-146>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "state"
                            ],
                            "properties": {
                              "state": {
                                "type": "string",
                                "enum": [
                                  "waitingforextensionservicesconfig"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-149>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-148>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "state"
                            ],
                            "properties": {
                              "state": {
                                "type": "string",
                                "enum": [
                                  "waitingforreboottoready"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-151>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-150>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "state"
                            ],
                            "properties": {
                              "state": {
                                "type": "string",
                                "enum": [
                                  "ready"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-153>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-152>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "platform_config_state",
                              "state"
                            ],
                            "properties": {
                              "platform_config_state": {
                                "oneOf": [
                                  {
                                    "type": "object",
                                    "required": [
                                      "power_on",
                                      "state"
                                    ],
                                    "properties": {
                                      "power_on": {
                                        "type": "boolean",
                                        "x-parser-schema-id": "<anonymous-schema-156>"
                                      },
                                      "state": {
                                        "type": "string",
                                        "enum": [
                                          "powercycle"
                                        ],
                                        "x-parser-schema-id": "<anonymous-schema-157>"
                                      }
                                    },
                                    "x-parser-schema-id": "<anonymous-schema-155>"
                                  },
                                  {
                                    "type": "object",
                                    "required": [
                                      "state"
                                    ],
                                    "properties": {
                                      "state": {
                                        "type": "string",
                                        "enum": [
                                          "checkhostconfig"
                                        ],
                                        "x-parser-schema-id": "<anonymous-schema-159>"
                                      }
                                    },
                                    "x-parser-schema-id": "<anonymous-schema-158>"
                                  },
                                  {
                                    "type": "object",
                                    "required": [
                                      "state"
                                    ],
                                    "properties": {
                                      "state": {
                                        "type": "string",
                                        "enum": [
                                          "unlockhost"
                                        ],
                                        "x-parser-schema-id": "<anonymous-schema-161>"
                                      }
                                    },
                                    "x-parser-schema-id": "<anonymous-schema-160>"
                                  },
                                  {
                                    "type": "object",
                                    "required": [
                                      "state"
                                    ],
                                    "properties": {
                                      "state": {
                                        "type": "string",
                                        "enum": [
                                          "configurebios"
                                        ],
                                        "x-parser-schema-id": "<anonymous-schema-163>"
                                      }
                                    },
                                    "x-parser-schema-id": "<anonymous-schema-162>"
                                  },
                                  {
                                    "type": "object",
                                    "required": [
                                      "state"
                                    ],
                                    "properties": {
                                      "state": {
                                        "type": "string",
                                        "enum": [
                                          "pollingbiossetup"
                                        ],
                                        "x-parser-schema-id": "<anonymous-schema-165>"
                                      }
                                    },
                                    "x-parser-schema-id": "<anonymous-schema-164>"
                                  },
                                  {
                                    "type": "object",
                                    "required": [
                                      "set_boot_order_info",
                                      "state"
                                    ],
                                    "properties": {
                                      "set_boot_order_info": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[2].properties.machine_state.oneOf[4].properties.set_boot_order_info.anyOf[0]",
                                      "state": {
                                        "type": "string",
                                        "enum": [
                                          "setbootorder"
                                        ],
                                        "x-parser-schema-id": "<anonymous-schema-167>"
                                      }
                                    },
                                    "x-parser-schema-id": "<anonymous-schema-166>"
                                  },
                                  {
                                    "type": "object",
                                    "required": [
                                      "state"
                                    ],
                                    "properties": {
                                      "state": {
                                        "type": "string",
                                        "enum": [
                                          "lockhost"
                                        ],
                                        "x-parser-schema-id": "<anonymous-schema-169>"
                                      }
                                    },
                                    "x-parser-schema-id": "<anonymous-schema-168>"
                                  }
                                ],
                                "x-parser-schema-id": "HostPlatformConfigurationState"
                              },
                              "state": {
                                "type": "string",
                                "enum": [
                                  "hostplatformconfiguration"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-170>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-154>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "state"
                            ],
                            "properties": {
                              "state": {
                                "type": "string",
                                "enum": [
                                  "waitingfordpustoup"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-172>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-171>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "state"
                            ],
                            "properties": {
                              "retry": {
                                "default": {
                                  "count": 0
                                },
                                "allOf": [
                                  {
                                    "type": "object",
                                    "required": [
                                      "count"
                                    ],
                                    "properties": {
                                      "count": {
                                        "type": "integer",
                                        "format": "uint64",
                                        "minimum": 0,
                                        "x-parser-schema-id": "<anonymous-schema-175>"
                                      }
                                    },
                                    "x-parser-schema-id": "RetryInfo"
                                  }
                                ],
                                "x-parser-schema-id": "<anonymous-schema-174>"
                              },
                              "state": {
                                "type": "string",
                                "enum": [
                                  "bootingwithdiscoveryimage"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-176>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-173>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "state"
                            ],
                            "properties": {
                              "state": {
                                "type": "string",
                                "enum": [
                                  "switchtoadminnetwork"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-178>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-177>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "state"
                            ],
                            "properties": {
                              "state": {
                                "type": "string",
                                "enum": [
                                  "waitingfornetworkreconfig"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-180>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-179>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "dpu_states",
                              "state"
                            ],
                            "properties": {
                              "dpu_states": {
                                "type": "object",
                                "required": [
                                  "states"
                                ],
                                "properties": {
                                  "states": {
                                    "type": "object",
                                    "additionalProperties": {
                                      "oneOf": [
                                        {
                                          "type": "string",
                                          "enum": [
                                            "firmwareupgrade",
                                            "waitingfornetworkinstall",
                                            "poweringoffhost",
                                            "powerdown",
                                            "buffertime",
                                            "verifyfirmareversions",
                                            "waitingfornetworkconfig",
                                            "reboothostbmc",
                                            "reboothost",
                                            "notunderreprovision"
                                          ],
                                          "x-parser-schema-id": "<anonymous-schema-183>"
                                        },
                                        {
                                          "type": "object",
                                          "required": [
                                            "bmcfirmwareupgrade"
                                          ],
                                          "properties": {
                                            "bmcfirmwareupgrade": {
                                              "type": "object",
                                              "required": [
                                                "substate"
                                              ],
                                              "properties": {
                                                "substate": {
                                                  "oneOf": [
                                                    {
                                                      "type": "object",
                                                      "required": [
                                                        "bmcfirmwareupdatesubstate"
                                                      ],
                                                      "properties": {
                                                        "bmcfirmwareupdatesubstate": {
                                                          "type": "string",
                                                          "enum": [
                                                            "checkfwversion"
                                                          ],
                                                          "x-parser-schema-id": "<anonymous-schema-187>"
                                                        }
                                                      },
                                                      "x-parser-schema-id": "<anonymous-schema-186>"
                                                    },
                                                    {
                                                      "type": "object",
                                                      "required": [
                                                        "bmcfirmwareupdatesubstate",
                                                        "firmware_type",
                                                        "task_id"
                                                      ],
                                                      "properties": {
                                                        "bmcfirmwareupdatesubstate": {
                                                          "type": "string",
                                                          "enum": [
                                                            "waitforupdatecompletion"
                                                          ],
                                                          "x-parser-schema-id": "<anonymous-schema-189>"
                                                        },
                                                        "firmware_type": {
                                                          "type": "string",
                                                          "enum": [
                                                            "bmc",
                                                            "cec",
                                                            "uefi",
                                                            "nic",
                                                            "cpldmb",
                                                            "cpldpdb",
                                                            "hgxbmc",
                                                            "combinedbmcuefi",
                                                            "gpu",
                                                            "unknown"
                                                          ],
                                                          "x-parser-schema-id": "FirmwareComponentType"
                                                        },
                                                        "task_id": {
                                                          "type": "string",
                                                          "x-parser-schema-id": "<anonymous-schema-190>"
                                                        }
                                                      },
                                                      "x-parser-schema-id": "<anonymous-schema-188>"
                                                    },
                                                    {
                                                      "type": "object",
                                                      "required": [
                                                        "bmcfirmwareupdatesubstate",
                                                        "count"
                                                      ],
                                                      "properties": {
                                                        "bmcfirmwareupdatesubstate": {
                                                          "type": "string",
                                                          "enum": [
                                                            "reboot"
                                                          ],
                                                          "x-parser-schema-id": "<anonymous-schema-192>"
                                                        },
                                                        "count": {
                                                          "type": "integer",
                                                          "format": "uint32",
                                                          "minimum": 0,
                                                          "x-parser-schema-id": "<anonymous-schema-193>"
                                                        }
                                                      },
                                                      "x-parser-schema-id": "<anonymous-schema-191>"
                                                    },
                                                    {
                                                      "type": "object",
                                                      "required": [
                                                        "bmcfirmwareupdatesubstate"
                                                      ],
                                                      "properties": {
                                                        "bmcfirmwareupdatesubstate": {
                                                          "type": "string",
                                                          "enum": [
                                                            "waitforerotbackgroundcopytocomplete"
                                                          ],
                                                          "x-parser-schema-id": "<anonymous-schema-195>"
                                                        }
                                                      },
                                                      "x-parser-schema-id": "<anonymous-schema-194>"
                                                    },
                                                    {
                                                      "type": "object",
                                                      "required": [
                                                        "bmcfirmwareupdatesubstate"
                                                      ],
                                                      "properties": {
                                                        "bmcfirmwareupdatesubstate": {
                                                          "type": "string",
                                                          "enum": [
                                                            "hostpowercycle"
                                                          ],
                                                          "x-parser-schema-id": "<anonymous-schema-197>"
                                                        }
                                                      },
                                                      "x-parser-schema-id": "<anonymous-schema-196>"
                                                    },
                                                    {
                                                      "type": "object",
                                                      "required": [
                                                        "bmcfirmwareupdatesubstate",
                                                        "failure_details"
                                                      ],
                                                      "properties": {
                                                        "bmcfirmwareupdatesubstate": {
                                                          "type": "string",
                                                          "enum": [
                                                            "failed"
                                                          ],
                                                          "x-parser-schema-id": "<anonymous-schema-199>"
                                                        },
                                                        "failure_details": {
                                                          "type": "string",
                                                          "x-parser-schema-id": "<anonymous-schema-200>"
                                                        }
                                                      },
                                                      "x-parser-schema-id": "<anonymous-schema-198>"
                                                    },
                                                    {
                                                      "type": "object",
                                                      "required": [
                                                        "bmcfirmwareupdatesubstate"
                                                      ],
                                                      "properties": {
                                                        "bmcfirmwareupdatesubstate": {
                                                          "type": "string",
                                                          "enum": [
                                                            "fwupdatecompleted"
                                                          ],
                                                          "x-parser-schema-id": "<anonymous-schema-202>"
                                                        }
                                                      },
                                                      "x-parser-schema-id": "<anonymous-schema-201>"
                                                    }
                                                  ],
                                                  "x-parser-schema-id": "BmcFirmwareUpgradeSubstate"
                                                }
                                              },
                                              "x-parser-schema-id": "<anonymous-schema-185>"
                                            }
                                          },
                                          "additionalProperties": false,
                                          "x-parser-schema-id": "<anonymous-schema-184>"
                                        },
                                        {
                                          "type": "object",
                                          "required": [
                                            "installdpuos"
                                          ],
                                          "properties": {
                                            "installdpuos": {
                                              "type": "object",
                                              "required": [
                                                "substate"
                                              ],
                                              "properties": {
                                                "substate": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[1].properties.dpu_states.properties.states.additionalProperties.oneOf[0].properties.substate"
                                              },
                                              "x-parser-schema-id": "<anonymous-schema-204>"
                                            }
                                          },
                                          "additionalProperties": false,
                                          "x-parser-schema-id": "<anonymous-schema-203>"
                                        }
                                      ],
                                      "x-parser-schema-id": "ReprovisionState"
                                    },
                                    "x-parser-schema-id": "<anonymous-schema-182>"
                                  }
                                },
                                "x-parser-schema-id": "DpuReprovisionStates"
                              },
                              "state": {
                                "type": "string",
                                "enum": [
                                  "dpureprovision"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-205>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-181>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "details",
                              "machine_id",
                              "state"
                            ],
                            "properties": {
                              "details": {
                                "type": "object",
                                "required": [
                                  "cause",
                                  "failed_at",
                                  "source"
                                ],
                                "properties": {
                                  "cause": {
                                    "oneOf": [
                                      {
                                        "type": "string",
                                        "enum": [
                                          "noerror"
                                        ],
                                        "x-parser-schema-id": "<anonymous-schema-207>"
                                      },
                                      {
                                        "type": "object",
                                        "required": [
                                          "nvmecleanfailed"
                                        ],
                                        "properties": {
                                          "nvmecleanfailed": {
                                            "type": "object",
                                            "required": [
                                              "err"
                                            ],
                                            "properties": {
                                              "err": {
                                                "type": "string",
                                                "x-parser-schema-id": "<anonymous-schema-210>"
                                              }
                                            },
                                            "x-parser-schema-id": "<anonymous-schema-209>"
                                          }
                                        },
                                        "additionalProperties": false,
                                        "x-parser-schema-id": "<anonymous-schema-208>"
                                      },
                                      {
                                        "type": "object",
                                        "required": [
                                          "discovery"
                                        ],
                                        "properties": {
                                          "discovery": {
                                            "type": "object",
                                            "required": [
                                              "err"
                                            ],
                                            "properties": {
                                              "err": {
                                                "type": "string",
                                                "x-parser-schema-id": "<anonymous-schema-213>"
                                              }
                                            },
                                            "x-parser-schema-id": "<anonymous-schema-212>"
                                          }
                                        },
                                        "additionalProperties": false,
                                        "x-parser-schema-id": "<anonymous-schema-211>"
                                      },
                                      {
                                        "type": "object",
                                        "required": [
                                          "reprovisioning"
                                        ],
                                        "properties": {
                                          "reprovisioning": {
                                            "type": "object",
                                            "required": [
                                              "err"
                                            ],
                                            "properties": {
                                              "err": {
                                                "type": "string",
                                                "x-parser-schema-id": "<anonymous-schema-216>"
                                              }
                                            },
                                            "x-parser-schema-id": "<anonymous-schema-215>"
                                          }
                                        },
                                        "additionalProperties": false,
                                        "x-parser-schema-id": "<anonymous-schema-214>"
                                      },
                                      {
                                        "type": "object",
                                        "required": [
                                          "machinevalidation"
                                        ],
                                        "properties": {
                                          "machinevalidation": {
                                            "type": "object",
                                            "required": [
                                              "err"
                                            ],
                                            "properties": {
                                              "err": {
                                                "type": "string",
                                                "x-parser-schema-id": "<anonymous-schema-219>"
                                              }
                                            },
                                            "x-parser-schema-id": "<anonymous-schema-218>"
                                          }
                                        },
                                        "additionalProperties": false,
                                        "x-parser-schema-id": "<anonymous-schema-217>"
                                      },
                                      {
                                        "type": "object",
                                        "required": [
                                          "unhandledstate"
                                        ],
                                        "properties": {
                                          "unhandledstate": {
                                            "type": "object",
                                            "required": [
                                              "err"
                                            ],
                                            "properties": {
                                              "err": {
                                                "type": "string",
                                                "x-parser-schema-id": "<anonymous-schema-222>"
                                              }
                                            },
                                            "x-parser-schema-id": "<anonymous-schema-221>"
                                          }
                                        },
                                        "additionalProperties": false,
                                        "x-parser-schema-id": "<anonymous-schema-220>"
                                      },
                                      {
                                        "type": "object",
                                        "required": [
                                          "measurementsfailedsignaturecheck"
                                        ],
                                        "properties": {
                                          "measurementsfailedsignaturecheck": {
                                            "type": "object",
                                            "required": [
                                              "err"
                                            ],
                                            "properties": {
                                              "err": {
                                                "type": "string",
                                                "x-parser-schema-id": "<anonymous-schema-225>"
                                              }
                                            },
                                            "x-parser-schema-id": "<anonymous-schema-224>"
                                          }
                                        },
                                        "additionalProperties": false,
                                        "x-parser-schema-id": "<anonymous-schema-223>"
                                      },
                                      {
                                        "type": "object",
                                        "required": [
                                          "measurementsretired"
                                        ],
                                        "properties": {
                                          "measurementsretired": {
                                            "type": "object",
                                            "required": [
                                              "err"
                                            ],
                                            "properties": {
                                              "err": {
                                                "type": "string",
                                                "x-parser-schema-id": "<anonymous-schema-228>"
                                              }
                                            },
                                            "x-parser-schema-id": "<anonymous-schema-227>"
                                          }
                                        },
                                        "additionalProperties": false,
                                        "x-parser-schema-id": "<anonymous-schema-226>"
                                      },
                                      {
                                        "type": "object",
                                        "required": [
                                          "measurementsrevoked"
                                        ],
                                        "properties": {
                                          "measurementsrevoked": {
                                            "type": "object",
                                            "required": [
                                              "err"
                                            ],
                                            "properties": {
                                              "err": {
                                                "type": "string",
                                                "x-parser-schema-id": "<anonymous-schema-231>"
                                              }
                                            },
                                            "x-parser-schema-id": "<anonymous-schema-230>"
                                          }
                                        },
                                        "additionalProperties": false,
                                        "x-parser-schema-id": "<anonymous-schema-229>"
                                      },
                                      {
                                        "type": "object",
                                        "required": [
                                          "measurementscavalidationfailed"
                                        ],
                                        "properties": {
                                          "measurementscavalidationfailed": {
                                            "type": "object",
                                            "required": [
                                              "err"
                                            ],
                                            "properties": {
                                              "err": {
                                                "type": "string",
                                                "x-parser-schema-id": "<anonymous-schema-234>"
                                              }
                                            },
                                            "x-parser-schema-id": "<anonymous-schema-233>"
                                          }
                                        },
                                        "additionalProperties": false,
                                        "x-parser-schema-id": "<anonymous-schema-232>"
                                      }
                                    ],
                                    "x-parser-schema-id": "FailureCause"
                                  },
                                  "failed_at": {
                                    "type": "string",
                                    "format": "date-time",
                                    "x-parser-schema-id": "<anonymous-schema-235>"
                                  },
                                  "source": {
                                    "oneOf": [
                                      {
                                        "type": "string",
                                        "enum": [
                                          "noerror",
                                          "scout",
                                          "statemachine"
                                        ],
                                        "x-parser-schema-id": "<anonymous-schema-236>"
                                      },
                                      {
                                        "type": "object",
                                        "required": [
                                          "statemachinearea"
                                        ],
                                        "properties": {
                                          "statemachinearea": {
                                            "type": "string",
                                            "enum": [
                                              "default",
                                              "hostinit",
                                              "mainflow",
                                              "assignedinstance"
                                            ],
                                            "x-parser-schema-id": "StateMachineArea"
                                          }
                                        },
                                        "additionalProperties": false,
                                        "x-parser-schema-id": "<anonymous-schema-237>"
                                      }
                                    ],
                                    "x-parser-schema-id": "FailureSource"
                                  }
                                },
                                "x-parser-schema-id": "FailureDetails"
                              },
                              "machine_id": {
                                "type": "string",
                                "x-parser-schema-id": "MachineId"
                              },
                              "state": {
                                "type": "string",
                                "enum": [
                                  "failed"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-238>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-206>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "reprovision_state",
                              "state"
                            ],
                            "properties": {
                              "reprovision_state": {
                                "oneOf": [
                                  {
                                    "type": "string",
                                    "enum": [
                                      "checkingfirmware",
                                      "checkingfirmwarerepeat"
                                    ],
                                    "x-parser-schema-id": "<anonymous-schema-240>"
                                  },
                                  {
                                    "type": "object",
                                    "required": [
                                      "initialreset"
                                    ],
                                    "properties": {
                                      "initialreset": {
                                        "type": "object",
                                        "required": [
                                          "last_time",
                                          "phase"
                                        ],
                                        "properties": {
                                          "last_time": {
                                            "type": "string",
                                            "format": "date-time",
                                            "x-parser-schema-id": "<anonymous-schema-243>"
                                          },
                                          "phase": {
                                            "type": "string",
                                            "enum": [
                                              "start",
                                              "bmcwasreset",
                                              "waithostboot"
                                            ],
                                            "x-parser-schema-id": "InitialResetPhase"
                                          }
                                        },
                                        "x-parser-schema-id": "<anonymous-schema-242>"
                                      }
                                    },
                                    "additionalProperties": false,
                                    "x-parser-schema-id": "<anonymous-schema-241>"
                                  },
                                  {
                                    "type": "object",
                                    "required": [
                                      "waitingforscript"
                                    ],
                                    "properties": {
                                      "waitingforscript": {
                                        "type": "object",
                                        "x-parser-schema-id": "<anonymous-schema-245>"
                                      }
                                    },
                                    "additionalProperties": false,
                                    "x-parser-schema-id": "<anonymous-schema-244>"
                                  },
                                  {
                                    "type": "object",
                                    "required": [
                                      "waitingforupload"
                                    ],
                                    "properties": {
                                      "waitingforupload": {
                                        "type": "object",
                                        "required": [
                                          "final_version",
                                          "firmware_type"
                                        ],
                                        "properties": {
                                          "final_version": {
                                            "type": "string",
                                            "x-parser-schema-id": "<anonymous-schema-248>"
                                          },
                                          "firmware_number": {
                                            "type": [
                                              "integer",
                                              "null"
                                            ],
                                            "format": "uint32",
                                            "minimum": 0,
                                            "x-parser-schema-id": "<anonymous-schema-249>"
                                          },
                                          "firmware_type": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[5].properties.instance_state.oneOf[14].properties.dpu_states.properties.states.additionalProperties.oneOf[1].properties.bmcfirmwareupgrade.properties.substate.oneOf[1].properties.firmware_type",
                                          "power_drains_needed": {
                                            "type": [
                                              "integer",
                                              "null"
                                            ],
                                            "format": "uint32",
                                            "minimum": 0,
                                            "x-parser-schema-id": "<anonymous-schema-250>"
                                          }
                                        },
                                        "x-parser-schema-id": "<anonymous-schema-247>"
                                      }
                                    },
                                    "additionalProperties": false,
                                    "x-parser-schema-id": "<anonymous-schema-246>"
                                  },
                                  {
                                    "type": "object",
                                    "required": [
                                      "waitingforfirmwareupgrade"
                                    ],
                                    "properties": {
                                      "waitingforfirmwareupgrade": {
                                        "type": "object",
                                        "required": [
                                          "final_version",
                                          "firmware_type",
                                          "task_id"
                                        ],
                                        "properties": {
                                          "final_version": {
                                            "type": "string",
                                            "x-parser-schema-id": "<anonymous-schema-253>"
                                          },
                                          "firmware_number": {
                                            "type": [
                                              "integer",
                                              "null"
                                            ],
                                            "format": "uint32",
                                            "minimum": 0,
                                            "x-parser-schema-id": "<anonymous-schema-254>"
                                          },
                                          "firmware_type": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[5].properties.instance_state.oneOf[14].properties.dpu_states.properties.states.additionalProperties.oneOf[1].properties.bmcfirmwareupgrade.properties.substate.oneOf[1].properties.firmware_type",
                                          "power_drains_needed": {
                                            "type": [
                                              "integer",
                                              "null"
                                            ],
                                            "format": "uint32",
                                            "minimum": 0,
                                            "x-parser-schema-id": "<anonymous-schema-255>"
                                          },
                                          "started_waiting": {
                                            "type": [
                                              "string",
                                              "null"
                                            ],
                                            "format": "date-time",
                                            "x-parser-schema-id": "<anonymous-schema-256>"
                                          },
                                          "task_id": {
                                            "type": "string",
                                            "x-parser-schema-id": "<anonymous-schema-257>"
                                          }
                                        },
                                        "x-parser-schema-id": "<anonymous-schema-252>"
                                      }
                                    },
                                    "additionalProperties": false,
                                    "x-parser-schema-id": "<anonymous-schema-251>"
                                  },
                                  {
                                    "type": "object",
                                    "required": [
                                      "resetfornewfirmware"
                                    ],
                                    "properties": {
                                      "resetfornewfirmware": {
                                        "type": "object",
                                        "required": [
                                          "final_version",
                                          "firmware_type"
                                        ],
                                        "properties": {
                                          "delay_until": {
                                            "type": [
                                              "integer",
                                              "null"
                                            ],
                                            "format": "int64",
                                            "x-parser-schema-id": "<anonymous-schema-260>"
                                          },
                                          "final_version": {
                                            "type": "string",
                                            "x-parser-schema-id": "<anonymous-schema-261>"
                                          },
                                          "firmware_type": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[5].properties.instance_state.oneOf[14].properties.dpu_states.properties.states.additionalProperties.oneOf[1].properties.bmcfirmwareupgrade.properties.substate.oneOf[1].properties.firmware_type",
                                          "last_power_drain_operation": {
                                            "anyOf": [
                                              {
                                                "type": "string",
                                                "enum": [
                                                  "off",
                                                  "powercycle",
                                                  "on"
                                                ],
                                                "x-parser-schema-id": "PowerDrainState"
                                              },
                                              {
                                                "type": "null",
                                                "x-parser-schema-id": "<anonymous-schema-263>"
                                              }
                                            ],
                                            "x-parser-schema-id": "<anonymous-schema-262>"
                                          },
                                          "power_drains_needed": {
                                            "type": [
                                              "integer",
                                              "null"
                                            ],
                                            "format": "uint32",
                                            "minimum": 0,
                                            "x-parser-schema-id": "<anonymous-schema-264>"
                                          }
                                        },
                                        "x-parser-schema-id": "<anonymous-schema-259>"
                                      }
                                    },
                                    "additionalProperties": false,
                                    "x-parser-schema-id": "<anonymous-schema-258>"
                                  },
                                  {
                                    "type": "object",
                                    "required": [
                                      "newfirmwarereportedwait"
                                    ],
                                    "properties": {
                                      "newfirmwarereportedwait": {
                                        "type": "object",
                                        "required": [
                                          "final_version",
                                          "firmware_type"
                                        ],
                                        "properties": {
                                          "final_version": {
                                            "type": "string",
                                            "x-parser-schema-id": "<anonymous-schema-267>"
                                          },
                                          "firmware_type": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[5].properties.instance_state.oneOf[14].properties.dpu_states.properties.states.additionalProperties.oneOf[1].properties.bmcfirmwareupgrade.properties.substate.oneOf[1].properties.firmware_type",
                                          "previous_reset_time": {
                                            "type": [
                                              "integer",
                                              "null"
                                            ],
                                            "format": "int64",
                                            "x-parser-schema-id": "<anonymous-schema-268>"
                                          }
                                        },
                                        "x-parser-schema-id": "<anonymous-schema-266>"
                                      }
                                    },
                                    "additionalProperties": false,
                                    "x-parser-schema-id": "<anonymous-schema-265>"
                                  },
                                  {
                                    "type": "object",
                                    "required": [
                                      "failedfirmwareupgrade"
                                    ],
                                    "properties": {
                                      "failedfirmwareupgrade": {
                                        "type": "object",
                                        "required": [
                                          "firmware_type"
                                        ],
                                        "properties": {
                                          "firmware_type": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[5].properties.instance_state.oneOf[14].properties.dpu_states.properties.states.additionalProperties.oneOf[1].properties.bmcfirmwareupgrade.properties.substate.oneOf[1].properties.firmware_type",
                                          "reason": {
                                            "type": [
                                              "string",
                                              "null"
                                            ],
                                            "x-parser-schema-id": "<anonymous-schema-271>"
                                          },
                                          "report_time": {
                                            "type": [
                                              "string",
                                              "null"
                                            ],
                                            "format": "date-time",
                                            "x-parser-schema-id": "<anonymous-schema-272>"
                                          }
                                        },
                                        "x-parser-schema-id": "<anonymous-schema-270>"
                                      }
                                    },
                                    "additionalProperties": false,
                                    "x-parser-schema-id": "<anonymous-schema-269>"
                                  }
                                ],
                                "x-parser-schema-id": "HostReprovisionState"
                              },
                              "state": {
                                "type": "string",
                                "enum": [
                                  "hostreprovision"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-273>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-239>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "network_config_update_state",
                              "state"
                            ],
                            "properties": {
                              "network_config_update_state": {
                                "description": "Tenant has requested network config update for the existing instance. At this point, instance config, instance network config version are already increased.",
                                "type": "string",
                                "enum": [
                                  "waitingfornetworksegmenttobeready",
                                  "waitingforconfigsynced",
                                  "releaseoldresources"
                                ],
                                "x-parser-schema-id": "NetworkConfigUpdateState"
                              },
                              "state": {
                                "type": "string",
                                "enum": [
                                  "networkconfigupdate"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-275>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-274>"
                          }
                        ],
                        "x-parser-schema-id": "InstanceState"
                      },
                      "state": {
                        "type": "string",
                        "enum": [
                          "assigned"
                        ],
                        "x-parser-schema-id": "<anonymous-schema-276>"
                      }
                    },
                    "x-parser-schema-id": "<anonymous-schema-135>"
                  },
                  {
                    "description": "Some cleanup is going on.",
                    "type": "object",
                    "required": [
                      "cleanup_state",
                      "state"
                    ],
                    "properties": {
                      "cleanup_state": {
                        "oneOf": [
                          {
                            "type": "object",
                            "required": [
                              "state"
                            ],
                            "properties": {
                              "state": {
                                "type": "string",
                                "enum": [
                                  "init"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-279>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-278>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "secure_erase_boss_context",
                              "state"
                            ],
                            "properties": {
                              "secure_erase_boss_context": {
                                "type": "object",
                                "required": [
                                  "boss_controller_id",
                                  "secure_erase_boss_state"
                                ],
                                "properties": {
                                  "boss_controller_id": {
                                    "type": "string",
                                    "x-parser-schema-id": "<anonymous-schema-281>"
                                  },
                                  "iteration": {
                                    "type": [
                                      "integer",
                                      "null"
                                    ],
                                    "format": "uint32",
                                    "minimum": 0,
                                    "x-parser-schema-id": "<anonymous-schema-282>"
                                  },
                                  "secure_erase_boss_state": {
                                    "oneOf": [
                                      {
                                        "type": "object",
                                        "required": [
                                          "state"
                                        ],
                                        "properties": {
                                          "state": {
                                            "type": "string",
                                            "enum": [
                                              "unlockhost"
                                            ],
                                            "x-parser-schema-id": "<anonymous-schema-284>"
                                          }
                                        },
                                        "x-parser-schema-id": "<anonymous-schema-283>"
                                      },
                                      {
                                        "type": "object",
                                        "required": [
                                          "state"
                                        ],
                                        "properties": {
                                          "state": {
                                            "type": "string",
                                            "enum": [
                                              "secureeraseboss"
                                            ],
                                            "x-parser-schema-id": "<anonymous-schema-286>"
                                          }
                                        },
                                        "x-parser-schema-id": "<anonymous-schema-285>"
                                      },
                                      {
                                        "type": "object",
                                        "required": [
                                          "state"
                                        ],
                                        "properties": {
                                          "state": {
                                            "type": "string",
                                            "enum": [
                                              "waitforjobcompletion"
                                            ],
                                            "x-parser-schema-id": "<anonymous-schema-288>"
                                          }
                                        },
                                        "x-parser-schema-id": "<anonymous-schema-287>"
                                      },
                                      {
                                        "type": "object",
                                        "required": [
                                          "failure",
                                          "power_state",
                                          "state"
                                        ],
                                        "properties": {
                                          "failure": {
                                            "type": "string",
                                            "x-parser-schema-id": "<anonymous-schema-290>"
                                          },
                                          "power_state": {
                                            "type": "string",
                                            "x-parser-schema-id": "<anonymous-schema-291>"
                                          },
                                          "state": {
                                            "type": "string",
                                            "enum": [
                                              "handlejobfailure"
                                            ],
                                            "x-parser-schema-id": "<anonymous-schema-292>"
                                          }
                                        },
                                        "x-parser-schema-id": "<anonymous-schema-289>"
                                      }
                                    ],
                                    "x-parser-schema-id": "SecureEraseBossState"
                                  },
                                  "secure_erase_jid": {
                                    "type": [
                                      "string",
                                      "null"
                                    ],
                                    "x-parser-schema-id": "<anonymous-schema-293>"
                                  }
                                },
                                "x-parser-schema-id": "SecureEraseBossContext"
                              },
                              "state": {
                                "type": "string",
                                "enum": [
                                  "secureeraseboss"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-294>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-280>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "state"
                            ],
                            "properties": {
                              "boss_controller_id": {
                                "type": [
                                  "string",
                                  "null"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-296>"
                              },
                              "state": {
                                "type": "string",
                                "enum": [
                                  "hostcleanup"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-297>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-295>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "create_boss_volume_context",
                              "state"
                            ],
                            "properties": {
                              "create_boss_volume_context": {
                                "type": "object",
                                "required": [
                                  "boss_controller_id",
                                  "create_boss_volume_state"
                                ],
                                "properties": {
                                  "boss_controller_id": {
                                    "type": "string",
                                    "x-parser-schema-id": "<anonymous-schema-299>"
                                  },
                                  "create_boss_volume_jid": {
                                    "type": [
                                      "string",
                                      "null"
                                    ],
                                    "x-parser-schema-id": "<anonymous-schema-300>"
                                  },
                                  "create_boss_volume_state": {
                                    "oneOf": [
                                      {
                                        "type": "object",
                                        "required": [
                                          "state"
                                        ],
                                        "properties": {
                                          "state": {
                                            "type": "string",
                                            "enum": [
                                              "createbossvolume"
                                            ],
                                            "x-parser-schema-id": "<anonymous-schema-302>"
                                          }
                                        },
                                        "x-parser-schema-id": "<anonymous-schema-301>"
                                      },
                                      {
                                        "type": "object",
                                        "required": [
                                          "state"
                                        ],
                                        "properties": {
                                          "state": {
                                            "type": "string",
                                            "enum": [
                                              "waitforjobscheduled"
                                            ],
                                            "x-parser-schema-id": "<anonymous-schema-304>"
                                          }
                                        },
                                        "x-parser-schema-id": "<anonymous-schema-303>"
                                      },
                                      {
                                        "type": "object",
                                        "required": [
                                          "state"
                                        ],
                                        "properties": {
                                          "state": {
                                            "type": "string",
                                            "enum": [
                                              "reboothost"
                                            ],
                                            "x-parser-schema-id": "<anonymous-schema-306>"
                                          }
                                        },
                                        "x-parser-schema-id": "<anonymous-schema-305>"
                                      },
                                      {
                                        "type": "object",
                                        "required": [
                                          "state"
                                        ],
                                        "properties": {
                                          "state": {
                                            "type": "string",
                                            "enum": [
                                              "waitforjobcompletion"
                                            ],
                                            "x-parser-schema-id": "<anonymous-schema-308>"
                                          }
                                        },
                                        "x-parser-schema-id": "<anonymous-schema-307>"
                                      },
                                      {
                                        "type": "object",
                                        "required": [
                                          "failure",
                                          "power_state",
                                          "state"
                                        ],
                                        "properties": {
                                          "failure": {
                                            "type": "string",
                                            "x-parser-schema-id": "<anonymous-schema-310>"
                                          },
                                          "power_state": {
                                            "type": "string",
                                            "x-parser-schema-id": "<anonymous-schema-311>"
                                          },
                                          "state": {
                                            "type": "string",
                                            "enum": [
                                              "handlejobfailure"
                                            ],
                                            "x-parser-schema-id": "<anonymous-schema-312>"
                                          }
                                        },
                                        "x-parser-schema-id": "<anonymous-schema-309>"
                                      },
                                      {
                                        "type": "object",
                                        "required": [
                                          "state"
                                        ],
                                        "properties": {
                                          "state": {
                                            "type": "string",
                                            "enum": [
                                              "lockhost"
                                            ],
                                            "x-parser-schema-id": "<anonymous-schema-314>"
                                          }
                                        },
                                        "x-parser-schema-id": "<anonymous-schema-313>"
                                      }
                                    ],
                                    "x-parser-schema-id": "CreateBossVolumeState"
                                  },
                                  "iteration": {
                                    "type": [
                                      "integer",
                                      "null"
                                    ],
                                    "format": "uint32",
                                    "minimum": 0,
                                    "x-parser-schema-id": "<anonymous-schema-315>"
                                  }
                                },
                                "x-parser-schema-id": "CreateBossVolumeContext"
                              },
                              "state": {
                                "type": "string",
                                "enum": [
                                  "createbossvolume"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-316>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-298>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "state"
                            ],
                            "properties": {
                              "state": {
                                "type": "string",
                                "enum": [
                                  "disablebiosbmclockdown"
                                ],
                                "x-parser-schema-id": "<anonymous-schema-318>"
                              }
                            },
                            "x-parser-schema-id": "<anonymous-schema-317>"
                          }
                        ],
                        "x-parser-schema-id": "CleanupState"
                      },
                      "state": {
                        "type": "string",
                        "enum": [
                          "waitingforcleanup"
                        ],
                        "x-parser-schema-id": "<anonymous-schema-319>"
                      }
                    },
                    "x-parser-schema-id": "<anonymous-schema-277>"
                  },
                  {
                    "description": "A forced deletion process has been triggered by the admin CLI State controller will no longer manage the Machine",
                    "type": "object",
                    "required": [
                      "state"
                    ],
                    "properties": {
                      "state": {
                        "type": "string",
                        "enum": [
                          "forcedeletion"
                        ],
                        "x-parser-schema-id": "<anonymous-schema-321>"
                      }
                    },
                    "x-parser-schema-id": "<anonymous-schema-320>"
                  },
                  {
                    "description": "A dummy state used to create DPU in beginning. State will sync to Init when host will be created.",
                    "type": "object",
                    "required": [
                      "state"
                    ],
                    "properties": {
                      "state": {
                        "type": "string",
                        "enum": [
                          "created"
                        ],
                        "x-parser-schema-id": "<anonymous-schema-323>"
                      }
                    },
                    "x-parser-schema-id": "<anonymous-schema-322>"
                  },
                  {
                    "description": "Machine moved to failed state. Recovery will be based on FailedCause",
                    "type": "object",
                    "required": [
                      "details",
                      "machine_id",
                      "state"
                    ],
                    "properties": {
                      "details": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[5].properties.instance_state.oneOf[15].properties.details",
                      "machine_id": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[5].properties.instance_state.oneOf[15].properties.machine_id",
                      "retry_count": {
                        "default": 0,
                        "type": "integer",
                        "format": "uint32",
                        "minimum": 0,
                        "x-parser-schema-id": "<anonymous-schema-325>"
                      },
                      "state": {
                        "type": "string",
                        "enum": [
                          "failed"
                        ],
                        "x-parser-schema-id": "<anonymous-schema-326>"
                      }
                    },
                    "x-parser-schema-id": "<anonymous-schema-324>"
                  },
                  {
                    "description": "State used to indicate that DPU reprovisioning is going on.",
                    "type": "object",
                    "required": [
                      "dpu_states",
                      "state"
                    ],
                    "properties": {
                      "dpu_states": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[5].properties.instance_state.oneOf[14].properties.dpu_states",
                      "state": {
                        "type": "string",
                        "enum": [
                          "dpureprovision"
                        ],
                        "x-parser-schema-id": "<anonymous-schema-328>"
                      }
                    },
                    "x-parser-schema-id": "<anonymous-schema-327>"
                  },
                  {
                    "description": "State used to indicate that host reprovisioning is going on",
                    "type": "object",
                    "required": [
                      "reprovision_state",
                      "state"
                    ],
                    "properties": {
                      "reprovision_state": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[5].properties.instance_state.oneOf[16].properties.reprovision_state",
                      "retry_count": {
                        "default": 0,
                        "type": "integer",
                        "format": "uint32",
                        "minimum": 0,
                        "x-parser-schema-id": "<anonymous-schema-330>"
                      },
                      "state": {
                        "type": "string",
                        "enum": [
                          "hostreprovision"
                        ],
                        "x-parser-schema-id": "<anonymous-schema-331>"
                      }
                    },
                    "x-parser-schema-id": "<anonymous-schema-329>"
                  },
                  {
                    "description": "State used to indicate the API is currently waiting on the machine to send attestation measurements, or waiting for measurements to match a valid/approved measurement bundle, before continuing on towards a Ready state.",
                    "type": "object",
                    "required": [
                      "measuring_state",
                      "state"
                    ],
                    "properties": {
                      "measuring_state": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[2].properties.machine_state.oneOf[6].properties.measuring_state",
                      "state": {
                        "type": "string",
                        "enum": [
                          "measuring"
                        ],
                        "x-parser-schema-id": "<anonymous-schema-333>"
                      }
                    },
                    "x-parser-schema-id": "<anonymous-schema-332>"
                  },
                  {
                    "type": "object",
                    "required": [
                      "measuring_state",
                      "state"
                    ],
                    "properties": {
                      "measuring_state": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[2].properties.machine_state.oneOf[6].properties.measuring_state",
                      "state": {
                        "type": "string",
                        "enum": [
                          "postassignedmeasuring"
                        ],
                        "x-parser-schema-id": "<anonymous-schema-335>"
                      }
                    },
                    "x-parser-schema-id": "<anonymous-schema-334>"
                  },
                  {
                    "type": "object",
                    "required": [
                      "bom_validating_state",
                      "state"
                    ],
                    "properties": {
                      "bom_validating_state": {
                        "oneOf": [
                          {
                            "type": "object",
                            "required": [
                              "MatchingSku"
                            ],
                            "properties": {
                              "MatchingSku": {
                                "description": "A context for passing information between states thoughout the BOM validation process.",
                                "type": "object",
                                "properties": {
                                  "machine_validation_context": {
                                    "type": [
                                      "string",
                                      "null"
                                    ],
                                    "x-parser-schema-id": "<anonymous-schema-338>"
                                  },
                                  "reboot_retry_count": {
                                    "type": [
                                      "integer",
                                      "null"
                                    ],
                                    "format": "int64",
                                    "x-parser-schema-id": "<anonymous-schema-339>"
                                  }
                                },
                                "x-parser-schema-id": "BomValidatingContext"
                              }
                            },
                            "additionalProperties": false,
                            "x-parser-schema-id": "<anonymous-schema-337>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "UpdatingInventory"
                            ],
                            "properties": {
                              "UpdatingInventory": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[14].properties.bom_validating_state.oneOf[0].properties.MatchingSku"
                            },
                            "additionalProperties": false,
                            "x-parser-schema-id": "<anonymous-schema-340>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "VerifyingSku"
                            ],
                            "properties": {
                              "VerifyingSku": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[14].properties.bom_validating_state.oneOf[0].properties.MatchingSku"
                            },
                            "additionalProperties": false,
                            "x-parser-schema-id": "<anonymous-schema-341>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "SkuVerificationFailed"
                            ],
                            "properties": {
                              "SkuVerificationFailed": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[14].properties.bom_validating_state.oneOf[0].properties.MatchingSku"
                            },
                            "additionalProperties": false,
                            "x-parser-schema-id": "<anonymous-schema-342>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "WaitingForSkuAssignment"
                            ],
                            "properties": {
                              "WaitingForSkuAssignment": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[14].properties.bom_validating_state.oneOf[0].properties.MatchingSku"
                            },
                            "additionalProperties": false,
                            "x-parser-schema-id": "<anonymous-schema-343>"
                          },
                          {
                            "type": "object",
                            "required": [
                              "SkuMissing"
                            ],
                            "properties": {
                              "SkuMissing": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[14].properties.bom_validating_state.oneOf[0].properties.MatchingSku"
                            },
                            "additionalProperties": false,
                            "x-parser-schema-id": "<anonymous-schema-344>"
                          }
                        ],
                        "x-parser-schema-id": "BomValidating"
                      },
                      "state": {
                        "type": "string",
                        "enum": [
                          "bomvalidating"
                        ],
                        "x-parser-schema-id": "<anonymous-schema-345>"
                      }
                    },
                    "x-parser-schema-id": "<anonymous-schema-336>"
                  }
                ],
                "x-parser-schema-id": "ManagedHostState"
              }
            },
            "x-parser-schema-id": "<anonymous-schema-2>"
          },
          "x-parser-unique-object-id": "stateChangeMessage"
        }
      },
      "x-parser-unique-object-id": "managedHostState"
    }
  },
  "operations": {
    "publishManagedHostStateChange": {
      "action": "send",
      "channel": "$ref:$.channels.managedHostState",
      "messages": [
        "$ref:$.channels.managedHostState.messages.stateChangeMessage"
      ],
      "description": "Publish managed host state change events when a host transitions between states in the lifecycle.\n",
      "x-parser-unique-object-id": "publishManagedHostStateChange"
    },
    "subscribeManagedHostStateChange": {
      "action": "receive",
      "channel": "$ref:$.channels.managedHostState",
      "messages": [
        "$ref:$.channels.managedHostState.messages.stateChangeMessage"
      ],
      "description": "Subscribe to managed host state change events for monitoring, orchestration, and automation purposes.\n",
      "x-parser-unique-object-id": "subscribeManagedHostStateChange"
    }
  },
  "components": {
    "messages": {
      "ManagedHostStateChangeMessage": "$ref:$.channels.managedHostState.messages.stateChangeMessage"
    },
    "schemas": {
      "ManagedHostState": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state",
      "BmcFirmwareUpgradeSubstate": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[5].properties.instance_state.oneOf[14].properties.dpu_states.properties.states.additionalProperties.oneOf[1].properties.bmcfirmwareupgrade.properties.substate",
      "BomValidating": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[14].properties.bom_validating_state",
      "BomValidatingContext": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[14].properties.bom_validating_state.oneOf[0].properties.MatchingSku",
      "CleanupState": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[6].properties.cleanup_state",
      "CreateBossVolumeContext": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[6].properties.cleanup_state.oneOf[3].properties.create_boss_volume_context",
      "CreateBossVolumeState": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[6].properties.cleanup_state.oneOf[3].properties.create_boss_volume_context.properties.create_boss_volume_state",
      "DpuDiscoveringState": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[0].properties.dpu_states.properties.states.additionalProperties",
      "DpuDiscoveringStates": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[0].properties.dpu_states",
      "DpuInitState": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[1].properties.dpu_states.properties.states.additionalProperties",
      "DpuInitStates": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[1].properties.dpu_states",
      "DpuReprovisionStates": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[5].properties.instance_state.oneOf[14].properties.dpu_states",
      "FailureCause": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[5].properties.instance_state.oneOf[15].properties.details.properties.cause",
      "FailureDetails": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[5].properties.instance_state.oneOf[15].properties.details",
      "FailureSource": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[5].properties.instance_state.oneOf[15].properties.details.properties.source",
      "FirmwareComponentType": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[5].properties.instance_state.oneOf[14].properties.dpu_states.properties.states.additionalProperties.oneOf[1].properties.bmcfirmwareupgrade.properties.substate.oneOf[1].properties.firmware_type",
      "HostPlatformConfigurationState": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[5].properties.instance_state.oneOf[9].properties.platform_config_state",
      "HostReprovisionState": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[5].properties.instance_state.oneOf[16].properties.reprovision_state",
      "InitialResetPhase": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[5].properties.instance_state.oneOf[16].properties.reprovision_state.oneOf[1].properties.initialreset.properties.phase",
      "InstallDpuOsState": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[1].properties.dpu_states.properties.states.additionalProperties.oneOf[0].properties.substate",
      "InstanceState": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[5].properties.instance_state",
      "LockdownInfo": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[2].properties.machine_state.oneOf[9].properties.lockdown_info",
      "LockdownMode": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[2].properties.machine_state.oneOf[9].properties.lockdown_info.properties.mode",
      "LockdownState": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[2].properties.machine_state.oneOf[9].properties.lockdown_info.properties.state",
      "MachineId": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[5].properties.instance_state.oneOf[15].properties.machine_id",
      "MachineState": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[2].properties.machine_state",
      "MachineValidatingState": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[3].properties.validation_state.oneOf[0].properties.machine_validation",
      "MeasuringState": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[2].properties.machine_state.oneOf[6].properties.measuring_state",
      "NetworkConfigUpdateState": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[5].properties.instance_state.oneOf[17].properties.network_config_update_state",
      "PerformPowerOperation": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[1].properties.dpu_states.properties.states.additionalProperties.oneOf[2].properties.substate",
      "PowerDrainState": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[5].properties.instance_state.oneOf[16].properties.reprovision_state.oneOf[5].properties.resetfornewfirmware.properties.last_power_drain_operation.anyOf[0]",
      "ReprovisionState": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[5].properties.instance_state.oneOf[14].properties.dpu_states.properties.states.additionalProperties",
      "RetryInfo": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[5].properties.instance_state.oneOf[11].properties.retry.allOf[0]",
      "SecureEraseBossContext": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[6].properties.cleanup_state.oneOf[1].properties.secure_erase_boss_context",
      "SecureEraseBossState": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[6].properties.cleanup_state.oneOf[1].properties.secure_erase_boss_context.properties.secure_erase_boss_state",
      "SetBootOrderInfo": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[2].properties.machine_state.oneOf[4].properties.set_boot_order_info.anyOf[0]",
      "SetBootOrderState": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[2].properties.machine_state.oneOf[4].properties.set_boot_order_info.anyOf[0].properties.set_boot_order_state",
      "SetSecureBootState": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[0].properties.dpu_states.properties.states.additionalProperties.oneOf[3].properties.enable_secure_boot_state",
      "StateMachineArea": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[5].properties.instance_state.oneOf[15].properties.details.properties.source.oneOf[1].properties.statemachinearea",
      "UefiSetupInfo": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[2].properties.machine_state.oneOf[5].properties.uefi_setup_info",
      "UefiSetupState": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[2].properties.machine_state.oneOf[5].properties.uefi_setup_info.properties.uefi_setup_state",
      "ValidationState": "$ref:$.channels.managedHostState.messages.stateChangeMessage.payload.properties.managed_host_state.oneOf[3].properties.validation_state"
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
  