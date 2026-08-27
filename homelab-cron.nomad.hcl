variable "image" {
  type    = string
  default = "sayze/homelab-cron:master"
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
        interval = "30s"
        timeout  = "5s"
      }
    }

    task "homelab-cron" {
      driver = "docker"

      config {
        image = var.image
        ports = ["http"]

        # Read-only bind mount of the entire host filesystem, same pattern
        # as jobs/newrelic.nomad.hcl. rslave propagates new host mounts
        # (e.g. a USB drive) into the container without a restart. This
        # service never writes to the host — :ro is load-bearing, not
        # incidental.
        volumes = [
          "/:/host:ro,rslave",
        ]
      }

      env {
        ADDR      = ":8080"
        HOST_ROOT = "/host"
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
