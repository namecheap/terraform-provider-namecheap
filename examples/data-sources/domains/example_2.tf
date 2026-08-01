data "namecheap_domains" "all" {
  list_type = "ALL"
}

resource "namecheap_domain_records" "spf" {
  for_each = { for d in data.namecheap_domains.all.domains : d.name => d }

  domain = each.value.name
  mode   = "MERGE"

  record {
    hostname = "@"
    type     = "TXT"
    address  = "v=spf1 include:spf.example.com -all"
  }
}
