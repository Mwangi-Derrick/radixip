# lib/python/examples/geolocation.py
# Run with: python examples/geolocation.py

from radixip import RadixEngine, version

print(f"\nRadixIP v{version()} — Geolocation Demo\n")

engine = RadixEngine()

# Populate the routing table
routes = [
    ("1.0.0.0/8",      "AU",      "Asia-Pacific"),
    ("5.0.0.0/8",      "DE",      "Europe"),
    ("8.8.0.0/16",     "US",      "North America"),
    ("192.168.0.0/16", "private", "RFC1918"),
    ("10.0.0.0/8",     "private", "RFC1918"),
]

for cidr, country, region in routes:
    engine.insert(cidr, {
        "value": country,
        "attributes": {"region": region, "cidr": cidr}
    })

print(f"Loaded {len(engine)} prefixes.\n")

# Test lookups
ips = ['1.1.1.1', '5.2.3.4', '8.8.8.8', '192.168.1.100', '172.16.0.1']
for ip in ips:
    match = engine.lookup(ip)
    if match:
        print(f"  {ip:<18} → {match['value']} ({match['attributes']['region']})")
    else:
        print(f"  {ip:<18} → no match")

print("\nStats:", engine.stats())
