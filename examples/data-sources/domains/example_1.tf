data "namecheap_domains" "all" {
  search_term = ""    # optional keyword filter
  list_type   = "ALL" # ALL | EXPIRING | EXPIRED
}

output "domain_names" {
  value = [for d in data.namecheap_domains.all.domains : d.name]
}
