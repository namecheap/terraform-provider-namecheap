data "namecheap_domain_records" "current" {
  domain = "example.com"
}

output "record_hostnames" {
  value = [for r in data.namecheap_domain_records.current.records : r.hostname]
}
