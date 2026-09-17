variable "image" {
  type    = string
  default = "sayze/homelab-cron:master"
}

variable "alert_email_from" {
  type    = string
  default = ""
}

variable "alert_email_to" {
  type    = string
  default = ""
}

variable "aws_region" {
  type    = string
  default = "us-east-1"
}

variable "consul_addr" {
  type = string
  # This task isn't on the host network (see the group's `network` block
  # below), so it can't reach the local Consul agent via 127.0.0.1.
  # ${attr.unique.network.ip-address} is a Nomad runtime variable resolved
  # per allocation to the placement node's own network IP — Consul runs as
  # an agent on every Nomad client in this cluster (see homelab's
  # provisioning/ansible/playbooks/provision.yml) and listens on all
  # interfaces (consul_client_addr: "0.0.0.0"), so this reaches whichever
  # node the allocation actually lands on. UFW's "Lan" profile
  # (provisioning/ansible/roles/network) already permits this from the
  # container's bridge subnet.
  default = "http://${attr.unique.network.ip-address}:8500"
}

job "homelab-cron" {
  datacenters = ["hl"]
  type        = "service"

  group "homelab-cron" {
    count = 1

    network {
      port "http" {
        to = 8080
      }
    }

    service {
      name     = "homelab-cron"
      port     = "http"
      provider = "consul"

      # No traefik tags here on purpose: this service has no public routes.
      # /health exists only for this check.
      check {
        type     = "http"
        path     = "/health"
        port     = "http"
        interval = "60s"
        timeout  = "5s"
      }
    }

    task "homelab-cron" {
      driver = "docker"

      vault {
        role = "nomad-workloads"
      }

      config {
        image = var.image
        ports = ["http"]

        # Read-only bind mount of the entire host filesystem.
        volumes = [
          "/:/host:ro,rslave",
        ]
      }

      env {
        ADDR      = ":8080"
        HOST_ROOT = "/host"

        ALERT_EMAIL_FROM = var.alert_email_from
        ALERT_EMAIL_TO   = var.alert_email_to
        AWS_REGION       = var.aws_region

        # Consul's HTTP API address, used by internal/consul.HTTPClient to
        # read Traefik/PostgreSQL/New Relic Infrastructure's deployed
        # version from Consul service meta — see var.consul_addr's default
        # above for why this resolves per allocation instead of being a
        # fixed address.
        CONSUL_ADDR = var.consul_addr
      }

      # AWS SES credentials for job alert emails (internal/mailer). Read
      # directly by the AWS SDK's own env chain, not by this service's own
      # config — see internal/mailer/ses.go and internal/config/config.go.
      template {
        data        = <<-EOF
          {{ with secret "secret/data/homelab/homelab-cron" }}
          AWS_ACCESS_KEY_ID="{{ .Data.data.aws_access_key_id }}"
          AWS_SECRET_ACCESS_KEY="{{ .Data.data.aws_secret_access_key }}"
          {{ end }}
        EOF
        destination = "secrets/env"
        env         = true
      }

      logs {
        max_files     = 3
        max_file_size = 10
      }

      resources {
        cpu    = 50
        memory = 64
      }
    }
  }
}
