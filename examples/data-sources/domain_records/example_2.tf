data "namecheap_domain_records" "current" {
  domain = "example.com"
}

resource "namecheap_domain_records" "managed" {
  domain = "example.com"
  mode   = "OVERWRITE"

  dynamic "record" {
    for_each = data.namecheap_domain_records.current.records
    content {
      hostname = record.value.hostname
      type     = record.value.type
      address  = record.value.address
      mx_pref  = record.value.mx_pref
      ttl      = record.value.ttl
    }
  }

  record {
    hostname = "new"
    type     = "A"
    address  = "10.0.0.9"
  }
}
