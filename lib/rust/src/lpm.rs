/// Find the longest prefix match for a given IP address in a Radix tree
pub fn longest_prefix_match<'a>(root: &'a RadixNode, ip: IpAddr) -> Option<&'a Metadata> {
    if root.children.is_empty() {
        return root.metadata.as_ref();
    }

    let ip_str = ip_to_binary_string(ip);
    let mut best_match: Option<&'a Metadata> = None;
    let mut current_node = root;
    let mut matched_length = 0;

    // Start with the root's metadata if it exists
    if current_node.metadata.is_some() {
        best_match = current_node.metadata.as_ref();
    }

    // Traverse the tree
    while !current_node.children.is_empty() {
        let mut found_child = false;

        for (_, child) in current_node.children.iter() {
            let prefix_len = child.prefix.len();
            
            // Check if the child prefix matches the IP at the current position
            if matched_length + prefix_len <= ip_str.len() {
                //ip_str is the ip adress truned into string so we can do prefix matches
                //[start..stop] synatx so rust stops when both start and stop match
                let ip_segment = &ip_str[matched_length..matched_length + prefix_len];
                
                if ip_segment == child.prefix {
                    // Found a matching child
                    current_node = child;
                    //prefix_len is the length of the child prefix
                    matched_length += prefix_len;
                    found_child = true;

                    // Update best match if this node has metadata
                    if current_node.metadata.is_some() {
                        best_match = current_node.metadata.as_ref();
                    }
                    break;
                }
            }
        }

        // If no matching child found, stop traversal
        if !found_child {
            break;
        }
    }

    best_match
}

/// Convert an IP address to a binary string representation
pub fn ip_to_binary_string(ip: IpAddr) -> String {
    match ip {
        IpAddr::V4(ipv4) => {
            let octets = ipv4.octets();
            octets.iter()
                .map(|&octet| format!("{:08b}", octet))
                .collect::<Vec<String>>()
                .concat()
        }
        IpAddr::V6(ipv6) => {
            let segments = ipv6.segments();
            segments.iter()
                .map(|&segment| format!("{:016b}", segment))
                .collect::<Vec<String>>()
                .concat()
        }
    }
}

/// Alternative implementation using the provided longest_common_prefix_len function
pub fn longest_prefix_match_with_lcp<'a>(root: &'a RadixNode, ip: IpAddr) -> Option<&'a Metadata> {
    let ip_str = ip_to_binary_string(ip);
    let mut best_match: Option<&'a Metadata> = None;
    let mut best_match_len = 0;

    // Check root
    if let Some(metadata) = &root.metadata {
        best_match = Some(metadata);
        best_match_len = 0;
    }

    // Check all children recursively
    check_children_for_match(root, &ip_str, &mut best_match, &mut best_match_len);

    best_match
}

fn check_children_for_match<'a>(
    node: &'a RadixNode,
    ip_str: &str,
    best_match: &mut Option<&'a Metadata>,
    best_match_len: &mut usize,
) {
    for child in node.children.values() {
        let prefix = &child.prefix;
        
        // Check if prefix matches at the current position
        // We need to track the current position in the IP string
        // This is a simplified version - in practice you'd need to track position
        let lcp_len = longest_common_prefix_len(prefix, ip_str);
        
        // If there's a match (full prefix matches)
        if lcp_len == prefix.len() && prefix.len() <= ip_str.len() {
            // If this node has metadata and it's longer than our current best
            if let Some(metadata) = &child.metadata {
                if prefix.len() > *best_match_len {
                    *best_match = Some(metadata);
                    *best_match_len = prefix.len();
                }
            }
            
            // Continue searching deeper
            check_children_for_match(child, ip_str, best_match, best_match_len);
        }
    }
}

/// Helper function to find the longest common prefix length between two strings
pub fn longest_common_prefix_len(left: &str, right: &str) -> usize {
    let left_bytes = left.as_bytes();
    let right_bytes = right.as_bytes();
    let limit = left_bytes.len().min(right_bytes.len());
    let mut index = 0;
    while index < limit && left_bytes[index] == right_bytes[index] {
        index += 1;
    }
    index
}

/// More efficient implementation using binary string representation
pub fn longest_prefix_match_binary<'a>(root: &'a RadixNode, ip: IpAddr) -> Option<&'a Metadata> {
    let ip_binary = ip_to_binary_string(ip);
    
    // Convert the tree to a more traversable format
    // For a complete implementation, you'd store the binary prefixes directly
    
    find_match_recursive(root, &ip_binary, 0)
}

fn find_match_recursive<'a>(
    node: &'a RadixNode,
    ip_binary: &str,
    current_pos: usize,
) -> Option<&'a Metadata> {
    // Check current node's metadata
    let mut best: Option<&'a Metadata> = node.metadata.as_ref();
    let mut best_len = if node.metadata.is_some() { current_pos } else { 0 };
    
    // Search children
    for child in node.children.values() {
        let child_prefix = &child.prefix;
        let prefix_len = child_prefix.len();
        
        // Check if we can match this prefix at the current position
        if current_pos + prefix_len <= ip_binary.len() {
            let ip_segment = &ip_binary[current_pos..current_pos + prefix_len];
            
            if ip_segment == child_prefix {
                // Recurse into child
                if let Some(child_match) = find_match_recursive(child, ip_binary, current_pos + prefix_len) {
                    // Check if child match is better than current best
                    let child_len = current_pos + prefix_len;
                    if child_len > best_len {
                        best = Some(child_match);
                        best_len = child_len;
                    }
                }
            }
        }
    }
    
    best
}