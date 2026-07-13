import json
import random
import ipaddress
import os

LOCATIONS = [
    {"country": "Kenya", "city": "Nairobi", "isp": "Safaricom"},
    {"country": "Kenya", "city": "Mombasa", "isp": "Jamii Telecommunications"},
    {"country": "United States", "city": "Ashburn", "isp": "AWS-East"},
    {"country": "Germany", "city": "Frankfurt", "isp": "Netcup GmbH"},
    {"country": "Ireland", "city": "Dublin", "isp": "Google Infrastructure"},
    {"country": "Singapore", "city": "Singapore", "isp": "Singtel"},
    {"country": "United Kingdom", "city": "London", "isp": "Linode LLC"},
    {"country": "Japan", "city": "Tokyo", "isp": "Sakura Internet"}
]

def generate_ipv4_subnet():
    """Generate a random IPv4 subnet"""
    o1 = random.randint(1, 223)
    o2 = random.randint(0, 255)
    o3 = random.randint(0, 255)
    mask = random.choice([8, 16, 24])
    return f"{o1}.{o2}.{o3}.0/{mask}"

def generate_ipv6_subnet():
    """Generate a random IPv6 subnet"""
    # Generate random IPv6 network with prefixes /32, /48, or /64
    parts = []
    for _ in range(8):
        parts.append(f"{random.randint(0, 65535):x}")
    
    prefix = random.choice([32, 48, 64])
    # For /32, keep first 4 hextets; for /48, keep first 6; for /64, keep all 8
    if prefix == 32:
        network = ":".join(parts[:4]) + "::"
    elif prefix == 48:
        network = ":".join(parts[:6]) + "::"
    else:  # /64
        network = ":".join(parts[:8]) + "::"
    
    return f"{network}/{prefix}"

def generate_ipv4_address_from_network(net):
    """Generate a random IPv4 address from within a network"""
    num_hosts = net.num_addresses
    random_offset = random.randint(0, num_hosts - 1) if num_hosts > 1 else 0
    return str(net.network_address + random_offset)

def generate_ipv6_address_from_network(net):
    """Generate a random IPv6 address from within a network"""
    num_hosts = net.num_addresses
    random_offset = random.randint(0, num_hosts - 1) if num_hosts > 1 else 0
    return str(net.network_address + random_offset)

def generate_miss_ipv4():
    """Generate a random IPv4 address that's likely to be a miss"""
    return f"{random.randint(224, 254)}.{random.randint(0,255)}.{random.randint(0,255)}.{random.randint(0,255)}"

def generate_miss_ipv6():
    """Generate a random IPv6 address that's likely to be a miss"""
    parts = []
    for _ in range(8):
        parts.append(f"{random.randint(0, 65535):x}")
    return ":".join(parts)

def generate_dataset(num_subnets=10000, num_lookups=100000):
    """Generate datasets with both IPv4 and IPv6 support"""
    
    # Split the datasets equally between IPv4 and IPv6
    num_ipv4_subnets = num_subnets // 2
    num_ipv6_subnets = num_subnets - num_ipv4_subnets
    
    ipv4_injections = []
    ipv6_injections = []
    ipv4_registered_networks = []
    ipv6_registered_networks = []
    
    print(f"Generating {num_ipv4_subnets} IPv4 subnets...")
    
    # Generate IPv4 subnets
    while len(ipv4_injections) < num_ipv4_subnets:
        try:
            subnet_str = generate_ipv4_subnet()
            net = ipaddress.ip_network(subnet_str, strict=False)
            net_str = str(net)
            
            if net_str not in [x["subnet"] for x in ipv4_injections]:
                loc = random.choice(LOCATIONS)
                ipv4_injections.append({
                    "subnet": net_str,
                    "version": 4,
                    "metadata": {
                        "country": loc["country"],
                        "city": loc["city"],
                        "isp": loc["isp"]
                    }
                })
                ipv4_registered_networks.append(net)
        except ValueError:
            continue
    
    print(f"Generating {num_ipv6_subnets} IPv6 subnets...")
    
    # Generate IPv6 subnets
    while len(ipv6_injections) < num_ipv6_subnets:
        try:
            subnet_str = generate_ipv6_subnet()
            net = ipaddress.ip_network(subnet_str, strict=False)
            net_str = str(net)
            
            if net_str not in [x["subnet"] for x in ipv6_injections]:
                loc = random.choice(LOCATIONS)
                ipv6_injections.append({
                    "subnet": net_str,
                    "version": 6,
                    "metadata": {
                        "country": loc["country"],
                        "city": loc["city"],
                        "isp": loc["isp"]
                    }
                })
                ipv6_registered_networks.append(net)
        except ValueError:
            continue
    
    # Combine all injections
    all_injections = ipv4_injections + ipv6_injections
    all_registered_networks = ipv4_registered_networks + ipv6_registered_networks
    
    print(f"Generating {num_lookups} lookups (80% hits, 20% misses)...")
    lookups = []
    
    num_hits = int(num_lookups * 0.8)
    num_misses = num_lookups - num_hits
    
    # Generate hits (mix of IPv4 and IPv6)
    num_ipv4_hits = num_hits // 2
    num_ipv6_hits = num_hits - num_ipv4_hits
    
    # IPv4 hits
    for _ in range(num_ipv4_hits):
        if ipv4_registered_networks:
            target_net = random.choice(ipv4_registered_networks)
            lookups.append({
                "ip": generate_ipv4_address_from_network(target_net),
                "type": "v4"
            })
    
    # IPv6 hits
    for _ in range(num_ipv6_hits):
        if ipv6_registered_networks:
            target_net = random.choice(ipv6_registered_networks)
            lookups.append({
                "ip": generate_ipv6_address_from_network(target_net),
                "type": "v6"
            })
    
    # Generate misses (mix of IPv4 and IPv6)
    num_ipv4_misses = num_misses // 2
    num_ipv6_misses = num_misses - num_ipv4_misses
    
    for _ in range(num_ipv4_misses):
        lookups.append({
            "ip": generate_miss_ipv4(),
            "type": "v4"
        })
    
    for _ in range(num_ipv6_misses):
        lookups.append({
            "ip": generate_miss_ipv6(),
            "type": "v6"
        })
    
    # Shuffle the lookups
    random.shuffle(lookups)
    
    # Create separate datasets for IPv4 and IPv6
    ipv4_lookups = [l for l in lookups if l["type"] == "v4"]
    ipv6_lookups = [l for l in lookups if l["type"] == "v6"]
    
    # Main payload with both versions
    payload = {
        "version": "1.0",
        "description": "Network dataset with IPv4 and IPv6 support",
        "ipv4": {
            "injections": ipv4_injections,
            "lookups": ipv4_lookups
        },
        "ipv6": {
            "injections": ipv6_injections,
            "lookups": ipv6_lookups
        },
        "combined": {
            "injections": all_injections,
            "lookups": lookups
        }
    }
    
    # Also save simplified format for backward compatibility
    simplified_payload = {
        "injections": all_injections,
        "lookups": [l["ip"] for l in lookups]
    }
    
    os.makedirs("benchmarks/data", exist_ok=True)
    
    # Save main dataset
    with open("benchmarks/data/mock_network_data.json", "w") as f:
        json.dump(payload, f, indent=2)
    
    # Save simplified dataset
    with open("benchmarks/data/mock_network_data_simple.json", "w") as f:
        json.dump(simplified_payload, f, indent=2)
    
    # Save separate datasets for convenience
    with open("benchmarks/data/ipv4_dataset.json", "w") as f:
        json.dump({
            "injections": ipv4_injections,
            "lookups": ipv4_lookups
        }, f, indent=2)
    
    with open("benchmarks/data/ipv6_dataset.json", "w") as f:
        json.dump({
            "injections": ipv6_injections,
            "lookups": ipv6_lookups
        }, f, indent=2)
    
    # Print statistics
    print("\nDataset Generation Complete!")
    print(f"Total IPv4 subnets: {len(ipv4_injections)}")
    print(f"Total IPv6 subnets: {len(ipv6_injections)}")
    print(f"Total IPv4 lookups: {len(ipv4_lookups)}")
    print(f"Total IPv6 lookups: {len(ipv6_lookups)}")
    print(f"Combined lookups: {len(lookups)}")
    print("\nFiles created:")
    print("  - benchmarks/data/mock_network_data.json (full dataset)")
    print("  - benchmarks/data/mock_network_data_simple.json (simplified)")
    print("  - benchmarks/data/ipv4_dataset.json (IPv4 only)")
    print("  - benchmarks/data/ipv6_dataset.json (IPv6 only)")

if __name__ == "__main__":
    generate_dataset()