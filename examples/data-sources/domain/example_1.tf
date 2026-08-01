data "namecheap_domain" "main" {
  domain = "example.com"
}

output "uses_namecheap_dns" {
  value = data.namecheap_domain.main.is_our_dns
}

output "nameservers" {
  value = data.namecheap_domain.main.nameservers
}

output "expires" {
  value = data.namecheap_domain.main.expires
}
