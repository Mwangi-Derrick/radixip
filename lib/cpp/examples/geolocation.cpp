// lib/cpp/examples/geolocation.cpp
//
// Build: g++ -std=c++17 -I../include geolocation.cpp -L../../../target/release -lradixip -o geolocation
// Run: ./geolocation

#include <iostream>
#include <iomanip>
#include <vector>
#include <string>
#include "RadixEngine.hpp"

struct Route {
    std::string cidr;
    std::string country;
    std::string region;
};

int main() {
    std::cout << "\nRadixIP v" << radixip::Engine::version() << " — Geolocation Demo\n\n";

    radixip::Engine engine;

    std::vector<Route> routes = {
        {"1.0.0.0/8",      "AU",      "Asia-Pacific"},
        {"5.0.0.0/8",      "DE",      "Europe"},
        {"8.8.0.0/16",     "US",      "North America"},
        {"192.168.0.0/16", "private", "RFC1918"},
        {"10.0.0.0/8",     "private", "RFC1918"}
    };

    for (const auto& r : routes) {
        engine.insert(r.cidr, {{"region", r.region}, {"cidr", r.cidr}}, r.country);
    }

    std::cout << "Loaded " << engine.size() << " prefixes.\n\n";

    std::vector<std::string> ips = {"1.1.1.1", "5.2.3.4", "8.8.8.8", "192.168.1.100", "172.16.0.1"};
    for (const auto& ip : ips) {
        auto match = engine.lookup(ip);
        std::cout << "  " << std::left << std::setw(18) << ip << " -> ";
        if (match) {
            std::cout << match->value << " (" << match->attributes["region"] << ")\n";
        } else {
            std::cout << "no match\n";
        }
    }

    std::cout << "\nDone.\n";
    return 0;
}
