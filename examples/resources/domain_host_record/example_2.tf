# A record per entry, driven by a map. Each instance owns exactly its own record,
# so adding or removing one key does not touch the others.
locals {
  hosts = {
    api = "203.0.113.20"
    cdn = "203.0.113.21"
  }
}

resource "namecheap_domain_host_record" "fleet" {
  for_each = local.hosts

  domain   = "example.com"
  hostname = each.key
  type     = "A"
  address  = each.value
}
