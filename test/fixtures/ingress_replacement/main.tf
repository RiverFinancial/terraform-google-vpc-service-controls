/**
 * Copyright 2026 Google LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

terraform {
  required_version = ">= 1.3"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "7.39.0"
    }
  }
}

provider "google" {
  # Plans use synthetic local state and never authenticate to GCP.
  access_token = "offline-plan-test-not-a-credential"
}

variable "use_legacy_list" {
  type = bool
}

variable "rule_title" {
  type    = string
  default = "original"
}

variable "identities" {
  type = list(string)
  default = [
    "serviceAccount:legacy@example.iam.gserviceaccount.com",
    "serviceAccount:retained@example.iam.gserviceaccount.com",
  ]
}

locals {
  rule = {
    title = var.rule_title
    from = {
      sources    = { resources = ["projects/456789012345"] }
      identities = var.identities
    }
    to = {
      operations = {
        "storage.googleapis.com" = { methods = ["*"] }
      }
    }
  }
}

module "perimeter" {
  source = "../../../modules/regular_service_perimeter"

  policy                  = "123456789012"
  perimeter_name          = "replacement_test"
  description             = "Synthetic ingress replacement plan test"
  restricted_services     = ["storage.googleapis.com"]
  vpc_accessible_services = []

  ingress_policies      = var.use_legacy_list ? [local.rule] : []
  ingress_policies_keys = var.use_legacy_list ? ["stable-rule"] : null
  ingress_policies_map  = var.use_legacy_list ? {} : { stable-rule = local.rule }
}
