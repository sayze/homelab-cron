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

    # api serves /health and GET /job/{name}. Triggering a job means
    # actually running it, so this task needs the same host mount, Docker
    # socket, and secrets cron does.
    task "api" {
      driver = "docker"

      vault {
        role = "nomad-workloads"
      }

      config {
        image        = var.image
        command      = "/usr/local/bin/api"
        network_mode = "host"

        volumes = [
          # Read-only bind mount of the entire host filesystem.
          "/:/host:ro,rslave",

          # Docker Engine API socket, for internal/docker's live version
          # check. NOTE: root-equivalent access, ":ro" doesn't restrict it
          # (see CLAUDE.md's "Host filesystem access").
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

        # CONSUL_ADDR/VAULT_ADDR/NOMAD_ADDR are deliberately unset here: on
        # the host network, internal/config's own defaults
        # (http://127.0.0.1:8500, :8200, and :4646, Consul's, Vault's, and
        # Nomad's own local-agent addresses) already resolve correctly,
        # same as traefik.nomad.hcl's
        # --providers.consulcatalog.endpoint.address.
      }

      # AWS SES credentials for a triggered job's alert email
      # (internal/mailer), and the ACL token internal/nomad.HTTPClient
      # sends as Nomad's X-Nomad-Token header to read Nomad's own deployed
      # version (internal/jobs.WebstackVersionCheck). AWS credentials are
      # read directly by the AWS SDK's own env chain, not by this
      # service's own config — see internal/mailer/ses.go and
      # internal/config/config.go.
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

      logs {
        max_files     = 3
        max_file_size = 10
      }

      resources {
        cpu    = 50
        memory = 64
      }
    }

    # cron builds and runs this service's actual cron jobs. It has no HTTP
    # surface of its own — see the api task above for /health and
    # GET /job/{name}.
    task "cron" {
      driver = "docker"

      vault {
        role = "nomad-workloads"
      }

      config {
        image        = var.image
        command      = "/usr/local/bin/cron"
        network_mode = "host"

        volumes = [
          # Read-only bind mount of the entire host filesystem.
          "/:/host:ro,rslave",

          # Docker Engine API socket, for internal/docker's live version
          # check. NOTE: root-equivalent access, ":ro" doesn't restrict it
          # (see CLAUDE.md's "Host filesystem access").
          "/var/run/docker.sock:/var/run/docker.sock",
        ]
      }

      env {
        HOST_ROOT = "/host"

        # DOCKER_SOCK is deliberately unset here: internal/config's own
        # default ("/var/run/docker.sock") already matches the volume mount
        # above.

        ALERT_EMAIL_FROM = var.alert_email_from
        ALERT_EMAIL_TO   = var.alert_email_to
        AWS_REGION       = var.aws_region

        # CONSUL_ADDR/VAULT_ADDR/NOMAD_ADDR are deliberately unset here: on
        # the host network, internal/config's own defaults
        # (http://127.0.0.1:8500, :8200, and :4646, Consul's, Vault's, and
        # Nomad's own local-agent addresses) already resolve correctly,
        # same as traefik.nomad.hcl's
        # --providers.consulcatalog.endpoint.address.
      }

      # AWS SES credentials for job alert emails (internal/mailer), and the
      # ACL token internal/nomad.HTTPClient sends as Nomad's X-Nomad-Token
      # header to read Nomad's own deployed version
      # (internal/jobs.WebstackVersionCheck). AWS credentials are read
      # directly by the AWS SDK's own env chain, not by this service's own
      # config — see internal/mailer/ses.go and internal/config/config.go.
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
