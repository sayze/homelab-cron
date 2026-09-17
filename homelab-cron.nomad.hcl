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

job "homelab-cron" {
  datacenters = ["hl"]
  type        = "service"

  group "homelab-cron" {
    count = 1

    # Host networking so this task can reach local agents/components directly
    network {
      mode = "host"

      port "http" {
        static = 8080
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
        image        = var.image
        network_mode = "host"

        volumes = [
          # Read-only bind mount of the entire host filesystem.
          "/:/host:ro,rslave",

          # The Docker Engine API's Unix socket, read by internal/docker so
          # WebstackVersionCheck can read the daemon's own deployed version
          # live instead of a hand-maintained baseline (see
          # internal/jobs/webstackversioncheck.go). NOTE: unlike the host
          # root mount above, ":ro" here only stops the container from
          # replacing/deleting the socket file itself — a process connected
          # to it still gets the full Docker Engine API, which is
          # root-equivalent on this host (e.g. it can create a privileged
          # container that mounts the host filesystem read-write). This is
          # the deliberate, explicit exception to this service's
          # read-only-host design that CLAUDE.md's "Host filesystem access"
          # section calls for; WebstackVersionCheck only ever calls
          # GET /version through it.
          "/var/run/docker.sock:/var/run/docker.sock",
        ]
      }

      env {
        ADDR      = ":8080"
        HOST_ROOT = "/host"

        # DOCKER_SOCK is deliberately unset here: internal/config's own
        # default ("/var/run/docker.sock") already matches the volume mount
        # above.

        ALERT_EMAIL_FROM = var.alert_email_from
        ALERT_EMAIL_TO   = var.alert_email_to
        AWS_REGION       = var.aws_region

        # CONSUL_ADDR/VAULT_ADDR are deliberately unset here: on the host
        # network, internal/config's own defaults (http://127.0.0.1:8500
        # and :8200, Consul's and Vault's own local-agent addresses)
        # already resolve correctly, same as traefik.nomad.hcl's
        # --providers.consulcatalog.endpoint.address.
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
