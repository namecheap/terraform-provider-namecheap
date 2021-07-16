---
page_title: Namecheap domain records guide
---

# Namecheap domain records guide

This guide shows how to create records for specific domain
using [namecheap_domain_records](../resources/domain_records.md) resource.

First of all we recommend to read our
article [Host records setup](https://www.namecheap.com/support/knowledgebase/article.aspx/434/2237/how-do-i-set-up-host-records-for-a-domain/)
to get an intro into all available record types provided by Namecheap and workarounds around it.

Follow [restrictions](#restrictions) section to read more about our API and Provider restrictions.

## Domain

Domain is required. Before using, you must buy the domain and be sure it is available on your Namecheap's Dashboard.

## Mode

The resource can work with two modes: `OVERWRITE` and `MERGE` (default).

### `MERGE`

Merge mode means that the records you have added via terraform will be merged with existing ones. Probably, you have
added some records manually, but would like to control for example A records via terraform, MERGE mode helps you to
achieve it.

When resource has been removed, the only records that was present in terraform resource will be removed from your
domain. The records that you have set manually will remain as is.

The same workflow works for both: `record` items and `nameservers`.

-> This is the default behavior, however, we recommend to set the mode explicitly.

~> Upon creating a new resource with the records or nameservers that already exist, the provider will throw a duplicate
error.

### `OVERWRITE`

Unlike [MERGE](#merge), `OVERWRITE` always removes existing records and force overwrites with provided in terraform
file.

Upon resource removing, all records will be destroyed. Upon removing the resource with `nameservers` the
default [Namecheap BasicDNS](https://www.namecheap.com/support/knowledgebase/article.aspx/923/10/what-is-your-basicdns/)
nameservers will be set

## Email type

Please check our
article [How can I set up MX records required for mail service?](https://www.namecheap.com/support/knowledgebase/article.aspx/322/2237/how-can-i-set-up-mx-records-required-for-mail-service/)
for better understanding the mail service workflow.

~> This section conflicts with `nameservers`. If you set up `email_type`, then you cannot set up custom nameservers and
vice-versa.

The `email_type` field is required if you want to set Mail Settings and MX records. After purchasing a domain, by
default `email_type` equals to `NONE`.

Normally, it takes 30 minutes for newly created records to take effect.

Here, you may choose one of the following Mail Settings depending on the mail service you wish to use:

### `NONE` (No Email Service)

if you wish to use no mail service. Your domain will have no MX records.

```terraform
resource "namecheap_domain_records" "my-domain-com" {
  domain = "my-domain.com"
  email_type = "NONE"
  #...
}
```

### `FWD` (Email Forwarding)

if you wish to create personalized e-mail addresses for a domain and forward emails to other email accounts of your
choice. The MX records will be set up automatically after selecting this option.

```terraform
resource "namecheap_domain_records" "my-domain-com" {
  domain = "my-domain.com"
  email_type = "FWD"
  #...
}
```

### `MXE`

is used for forwarding mail to a mail server's IP address. When you set `email_type = "MXE"`, the one MXE record is
required.

```terraform
resource "namecheap_domain_records" "my-domain-com" {
  domain = "my-domain.com"
  email_type = "MXE"

  record {
    hostname = "kit-kitty.live"
    type = "MXE"
    address = "12.13.14.15"
  }

  #...
}
```

### `MX` (Custom MX records)

is used to set MX records for third-party mail services, like cPanel webmail service (if you wish to use cPanel mail
service with default nameservers), Zoho mail, Outlook.com, etc.

```terraform
resource "namecheap_domain_records" "my-domain-com" {
  domain = "my-domain.com"
  email_type = "MX"

  record {
    hostname = "@"
    type = "MX"
    address = "mx.zoho.com."
  }

  #...
}
```

It is possible to indicate your own domain as the mail server address like mail.domain.tld or domain.tld. Please note
that the corresponding A record pointing to the IP address of the mail server should be created in the DNS settings:

```terraform
resource "namecheap_domain_records" "my-domain-com" {
  domain = "my-domain.com"
  email_type = "MX"

  record {
    hostname = "mail"
    type = "A"
    address = "123.45.67.89"
  }

  record {
    hostname = "@"
    type = "MX"
    address = "mail.my-domain.com."
    mx_pref = 10
  }

  #...
}
```

### `OX` (Private Mail)

if you wish to set up MX records for the [Namecheap Private email service](https://www.namecheap.com/hosting/email/).
The MX records will be set up automatically after selecting this option.

```terraform
resource "namecheap_domain_records" "my-domain-com" {
  domain = "my-domain.com"
  email_type = "OX"
  #...
}
```

### `GMAIL`

if you have a G Suite subscription, select the Gmail option to set up the records needed for this mail service.

```terraform
resource "namecheap_domain_records" "my-domain-com" {
  domain = "my-domain.com"
  email_type = "GMAIL"
  #...
}
```

## Host Record

This works only for the domains using Namecheap BasicDNS, FreeDNS
or [PremiumDNS](https://www.namecheap.com/security/premiumdns/).

~> This section conflicts with `nameservers`. If you set up `record` items, then you cannot set up custom nameservers
and vice-versa.

-> If you just bought the domain, you have default parking page (parking records) enabled. Upon creating a resource via
terraform, the default parking records will be removed.

**Host:** If you need to create a record for a bare domain (e.g., mydomain.tld), it is needed to put `@` in this field.
In case a record for any subdomain (like www.mydomain.tld or blog.mydomain.tld) should be created, put only the name of
your subdomain into the Host field without mentioning the domain itself. As such, the record for www.mydomain.tld should
have only www in the Host:

```terraform
resource "namecheap_domain_records" "my-domain-com" {
  domain = "my-domain.com"

  # for blog.my-domain.com
  record {
    hostname = "blog"
    type = "A"
    address = "23.236.62.147"
  }

  # for my-domain.com
  record {
    hostname = "@"
    type = "ALIAS"
    address = "www.testdomain.co.uk."
    ttl = 300
  }

  # for www.my-domain.com
  record {
    hostname = "www"
    type = "CNAME"
    address = "www245.wixdns.net."
  }

  #...
}
```

TTL: A TTL value of 1800 seconds (30 minutes) would mean that, if a DNS record was changed on the nameserver, DNS
servers around the world could still be showing the old value from their cache for up to 30 minutes after the change.
Our default TTL is 30 minutes.

## Nameservers

If you wish to point your domain to custom nameservers (for example, your Personal DNS servers or third-party hosting
nameservers if your domain is hosted with another DNS provider). You will need to set `nameservers` array as shown here:

```terraform
resource "namecheap_domain_records" "my-domain-com" {
  domain = "my-domain.com"
  mode = "OVERWRITE"

  nameservers = [
    "ns1.your-nameserver.com",
    "ns2.your-nameserver.com"
  ]
}
```

_*ns1-2.nameserver.com are used as an example. Please use the nameservers provided by your hosting/DNS provider._

-> It's required to enter the nameservers in the ns1.example.tld format, if you enter the IP addresses instead, the
system will not accept this. Thus, if you were provided with both the nameservers and IP address(es), only the
nameservers should be inserted as custom nameservers.

-> It's recommended to use mode `OVERWRITE` with `nameservers` to prevent unobvious behavior.

Nameservers changes do not propagate instantly. Once your nameservers are changed, it may take up to 24 hours (more, in
rare cases) for local ISPs to update their DNS caches so that everyone can see your website.

You can always check your domain name using any Proxy server as Proxy servers do not store cache, thus you can see the
non-cached information.

## Restrictions

Unfortunately, you're not able to create the following record types: `SRV`, `A + Dynamic DNS Record` due to our API
restrictions. For this case you can use `MERGE` mode - set up `SRV` or `Dynamic DNS Record` manually and control other
records via terraform.
