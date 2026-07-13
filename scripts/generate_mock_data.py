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

def generate_dataset(num_subnets=10000, num_lookups=100000):
    injections = []
    registered_networks = []
    
    print(f"Generating {num_subnets} subnets...")
    
    while len(injections) < num_subnets:
        o1 = random.randint(1, 223)
        o2 = random.randint(0, 255)
        o3 = random.randint(0, 255)
        mask = random.choice([8, 16, 24])
        
        try:
            net = ipaddress.ip_network(f"{o1}.{o2}.{o3}.0/{mask}", strict=False)
            net_str = str(net)
            
            if net_str not in [x["subnet"] for x in injections]:
                loc = random.choice(LOCATIONS)
                injections.append({
                    "subnet": net_str,
                    "metadata": {
                        "country": loc["country"],
                        "city": loc["city"],
                        "isp": loc["isp"]
                    }
                })
                registered_networks.append(net)
        except ValueError:
            continue

    print(f"Generating {num_lookups} lookups (80% hits, 20% misses)...")
    lookups = []
    
    num_hits = int(num_lookups * 0.8)
    num_misses = num_lookups - num_hits
    
    for _ in range(num_hits):
        target_net = random.choice(registered_networks)
        num_hosts = target_net.num_addresses
        random_offset = random.randint(0, num_hosts - 1) if num_hosts > 1 else 0
        ip_addr = target_net.network_address + random_offset
        lookups.append(str(ip_addr))
        
    while len(lookups) < num_lookups:
        miss_ip = f"{random.randint(224, 254)}.{random.randint(0,255)}.{random.randint(0,255)}.{random.randint(0,255)}"
        lookups.append(miss_ip)

    random.shuffle(lookups)

    payload = {
        "injections": injections,
        "lookups": lookups
    }
    
    os.makedirs("benchmarks/data", exist_ok=True)
    with open("benchmarks/data/mock_network_data.json", "w") as f:
        json.dump(payload, f, indent=2)
        
    print("Dataset generation complete!")

if __name__ == "__main__":
    generate_dataset()
