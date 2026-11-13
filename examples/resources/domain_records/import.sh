# The domain name imports the domain's mail mode (email_type), no records and no
# custom nameservers. The ones the configuration declares are adopted on the first apply.
terraform import namecheap_domain_records.main example.com

# <domain>/zone imports every record of the zone as well, or every custom
# nameserver of a delegated domain, for a configuration that declares all of them.
terraform import namecheap_domain_records.main example.com/zone
