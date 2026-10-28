data "namecheap_domains" "all" {
  # One extra API call per domain: is_locked reports the registrar lock.
  include_registrar_lock = true
}

output "unlocked_domains" {
  value = [for d in data.namecheap_domains.all.domains : d.name if !d.is_locked]
}
