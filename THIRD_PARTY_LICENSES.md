Third-Party Licenses for DSX Exchange Helm Charts
=================================================

This file lists the third-party Helm chart dependencies bundled with
DSX Exchange and their respective licenses. Versions correspond to those
declared in the charts under `deploy/`. This file is maintained manually.
Please update this file when adding or removing dependencies.


nats v2.12.6
  Source: https://github.com/nats-io/k8s (helm/charts/nats)
  Repository: https://nats-io.github.io/k8s/helm/charts/
  License: Apache-2.0
  Copyright: The NATS Authors

nack v0.33.2
  Source: https://github.com/nats-io/nack
  Repository: https://nats-io.github.io/k8s/helm/charts/
  License: Apache-2.0
  Copyright: The NATS Authors

surveyor v0.20.7
  Source: https://github.com/nats-io/k8s (helm/charts/surveyor)
  Repository: https://nats-io.github.io/k8s/helm/charts/
  License: Apache-2.0
  Copyright: The NATS Authors

auth-callout v0.1.1
  Source: https://github.com/NVIDIA/dsx-exchange/tree/main/auth-callout
  Repository: file://../../auth-callout/deploy
  License: Apache-2.0
  Copyright: NVIDIA CORPORATION & AFFILIATES

agentgateway v1.4.1
  Source: https://github.com/agentgateway/agentgateway
  Repository: oci://cr.agentgateway.dev/charts
  License: Apache-2.0
  Copyright: The agentgateway Authors

valkey v0.9.4
  Source: https://github.com/valkey-io/valkey-helm
  Repository: https://valkey-io.github.io/valkey-helm
  License: BSD-3-Clause
  Copyright: The Valkey Contributors
