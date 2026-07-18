'use strict';
// lib/node/examples/geolocation.js
// Quick demo showing the RadixIP engine in action.
// Run: node examples/geolocation.js (after npm run build)

const { RadixIP, version } = require('..');

console.log(`\nRadixIP v${version()} — Geolocation Demo\n`);

const engine = new RadixIP();

// Populate the routing table
const routes = [
  { cidr: '1.0.0.0/8',      country: 'AU', region: 'Asia-Pacific' },
  { cidr: '5.0.0.0/8',      country: 'DE', region: 'Europe'       },
  { cidr: '8.8.0.0/16',     country: 'US', region: 'North America' },
  { cidr: '192.168.0.0/16', country: 'private', region: 'RFC1918' },
  { cidr: '10.0.0.0/8',     country: 'private', region: 'RFC1918' },
];

for (const { cidr, country, region } of routes) {
  engine.insert(cidr, { value: country, attributes: { region, cidr } });
}

console.log(`Loaded ${engine.size} prefixes.\n`);

// Test lookups
const ips = ['1.1.1.1', '5.2.3.4', '8.8.8.8', '192.168.1.100', '172.16.0.1'];
for (const ip of ips) {
  const match = engine.lookup(ip);
  if (match) {
    console.log(`  ${ip.padEnd(18)} → ${match.value} (${match.attributes.region})`);
  } else {
    console.log(`  ${ip.padEnd(18)} → no match`);
  }
}

console.log('\nStats:', engine.stats());
