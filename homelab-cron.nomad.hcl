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
  default = "ap-southeast-2"
}

variable "db_user" {
  type    = string
  default = "homelab-cron"
}

variable "db_name" {
  type    = string
  default = "homelab"
}

job "homelab-cron" {
  datacenters = ["hl"]
  type        = "service"

  group "homelab-cron" {
    count = 1

    # Host networking so both tasks can reach local agents/components
    # directly.
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

    # api serves /health and GET /job/{name}; cron runs the scheduled jobs.
    # Both run jobs, so they share one definition and differ only by
    # binary.
    dynamic "task" {
      for_each = ["homelab-cron-api", "homelab-cron"]
      labels   = [task.value]

      content {
        driver = "docker"

        vault {
          role = "nomad-workloads"
        }

        config {
          image        = var.image
          command      = "/usr/local/bin/${task.value}"
          network_mode = "host"

          volumes = [
            # Read-only bind mount of the host filesystem.
            "/:/host:ro,rslave",

            # Docker Engine API socket. Root-equivalent access, ":ro"
            # doesn't restrict it (see CLAUDE.md).
            "/var/run/docker.sock:/var/run/docker.sock",
          ]
        }

        # Unset vars (DOCKER_SOCK, CONSUL_ADDR, VAULT_ADDR, NOMAD_ADDR) use
        # internal/config's defaults, which are correct on the host network.
        # ADDR is only read by api.
        env {
          ADDR             = ":8080"
          HOST_ROOT        = "/host"
          ALERT_EMAIL_FROM = var.alert_email_from
          ALERT_EMAIL_TO   = var.alert_email_to
          AWS_REGION       = var.aws_region
        }

        # AWS SES credentials and the Nomad ACL token.
        template {
          data        = <<-EOF
            {{ with secret "secret/data/homelab/homelab-cron" }}
            AWS_ACCESS_KEY_ID="{{ .Data.data.aws_access_key_id }}"
            AWS_SECRET_ACCESS_KEY="{{ .Data.data.aws_secret_access_key }}"
            NOMAD_TOKEN="{{ .Data.data.nomad_token }}"
            {{ end }}
          EOF
          destination = "secrets/env"
          env         = true
        }

        # DATABASE_URL. "postgres|any" includes unhealthy instances so a
        # failing postgres doesn't re-render this and restart the task
        # while the health check needs to report it.
        template {
          data        = <<-EOF
            {{ with secret "secret/data/homelab/homelab-cron" }}
            {{ $password := .Data.data.db_password }}
            {{ range service "postgres|any" }}
            DATABASE_URL="postgres://${var.db_user}:{{ $password | urlquery }}@{{ .Address }}:{{ .Port }}/${var.db_name}"
            {{ end }}
            {{ end }}
          EOF
          destination = "secrets/database.env"
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
}
