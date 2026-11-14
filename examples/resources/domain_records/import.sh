# The domain name imports the domain's settings (email_type, custom nameservers)
# and no records. Records the configuration declares are adopted on the first apply.
terraform import namecheap_domain_records.main example.com

# <domain>/zone imports every record of the zone as well, for a configuration
# that declares all of them.
terraform import namecheap_domain_records.main example.com/zone
