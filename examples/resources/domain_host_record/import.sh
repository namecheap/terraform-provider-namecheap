# The ID is <domain>/<type>/<hostname>/<address>. The address may itself contain
# slashes (a URL record, for instance) — only the first three components are split.
terraform import namecheap_domain_host_record.www example.com/A/www/203.0.113.10
