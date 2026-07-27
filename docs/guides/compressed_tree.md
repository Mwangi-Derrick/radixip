## Compressed Tree Insert

```go
/*
    ============================================================================
    COMPRESSED TREE INSERT - MAIN LOGIC
    ============================================================================
    
    We are inserting: 192.168.2.0/24
    Binary key:       11000000.10101000.00000010
    
    Current node (root) has:
    - edgeBits: 11000000.10101000.00000001 (24 bits) ← from 192.168.1.0/24
    - edgeLen: 24
    - metadata: "Home"
    
    We compare node's edge with our key:
    edgeBits: 11000000.10101000.00000001
    key:      11000000.10101000.00000010
              ^^^^^^^^^^^^^^^^^^^^ ^^^^^^
              First 22 MATCH        Last 2 DIFFER!
    
    shared = 22 (bits 0-21 match)
    edgeLen = 24 (total bits in node)
    remaining = 24 (total bits in our key)
    
    shared < edgeLen? 22 < 24 → TRUE! We need to SPLIT!
    ============================================================================
*/

if shared < edgeLen {
    /*
        STEP 1: Find the PIVOT BIT
        -------------------------
        The pivot bit is the FIRST bit where the paths diverge.
        It tells us which side the OLD node's remaining bits should go.
        
        edgeBits: 11000000.10101000.00000001
        position: 0        8        16       22 23
                  |        |        |        |  |
                  11000000 10101000 00000001  0  1
                                              ↑  ↑
                                              |  bit 23
                                              bit 22 (pivot!)
        
        pivotBit = getBitFromBytes(edgeBits, shared)
                 = getBitFromBytes(edgeBits, 22)
                 = 0 (since at position 22, the bit is 0)
    */
    pivotBit := getBitFromBytes(edgeBits, shared)
    
    /*
        STEP 2: Create CHILD for OLD node's remaining bits
        ------------------------------------------------
        The old node had: "11000000.10101000.00000001" (24 bits)
        Shared prefix:    "11000000.10101000.000000"   (22 bits)
        
        The OLD node's REMAINING bits after the split:
        - Bits from position shared+1 to end
        - shared+1 = 23, edgeLen-1 = 23
        - Extract from position 23, length 0
        
        oldRemainder = extractBits(edgeBits, 23, 0) = "" (empty)
        
        Why empty? Because the only bit after the split is bit 23,
        and we extract from position 23 with length 0 → NOTHING!
        
        Wait... let me recalculate:
        edgeBits = 11000000.10101000.00000001
        Positions: 0-21 = 11000000.10101000.000000 (22 bits)
                   position 22 = 0 (pivot)
                   position 23 = 1 (last bit)
        
        Old remainder = extractBits(edgeBits, 23, 1) = "1"
        BUT! The code does: extractBits(edgeBits, shared+1, edgeLen-shared-1)
                          = extractBits(edgeBits, 23, 24-22-1)
                          = extractBits(edgeBits, 23, 1)
                          = "1" ✓
        
        So child gets: edge = "1", metadata = "Home"
    */
    child := t.nodeBuilder.Build()
    child.SetEdge(extractBits(edgeBits, shared+1, edgeLen-shared-1), edgeLen-shared-1)
    child.SetMetadata(n.Metadata())  // "Home"
    child.SetPrefix(n.Prefix())      // 192.168.1.0/24
    child.SetLeft(n.Left())          // nil
    child.SetRight(n.Right())        // nil
    
    /*
        STEP 3: TRIM the current node to shared prefix
        ---------------------------------------------
        The shared prefix becomes the NEW edge of this node.
        
        old: edge = "11000000.10101000.00000001" (24 bits)
        new: edge = "11000000.10101000.000000"   (22 bits)
        
        This node is now just a SPLITTER - no metadata!
    */
    n.SetEdge(extractBits(edgeBits, 0, shared), shared)  // First 22 bits
    n.ClearMetadata()  // No metadata at splitter
    n.SetPrefix(nil)   // No prefix at splitter
    n.SetLeft(nil)     // Clear children
    n.SetRight(nil)    // Clear children
    
    /*
        STEP 4: Place the OLD node based on PIVOT BIT
        --------------------------------------------
        pivotBit = 0 → OLD node goes to LEFT
        
        Tree so far:
        Root: edge = "11000000.10101000.000000" (22 bits)
              metadata = nil (splitter)
              └─ LEFT: edge = "1", metadata = "Home" (192.168.1.0)
    */
    if pivotBit == 0 {
        n.SetLeft(child)   // OLD goes LEFT (pivotBit = 0)
    } else {
        n.SetRight(child)  // OLD goes RIGHT (pivotBit = 1)
    }
    
    /*
        STEP 5: Check if we've reached the end of our key
        ------------------------------------------------
        shared = 22, remaining = 24
        shared == remaining? 22 == 24 → FALSE!
        
        We still have bits left in our key!
        Our key: "11000000.10101000.00000010"
        Already matched: "11000000.10101000.000000" (22 bits)
        Remaining bits: "10" (bits 22-23)
    */
    if shared == remaining {
        n.SetMetadata(meta)
        n.SetPrefix(prefix)
        return true
    }
    
    /*
        STEP 6: Handle NEW key's remaining bits
        ---------------------------------------
        keyRem = "11000000.10101000.00000010" (24 bits)
        shared = 22
        
        newBit = getBitFromBytes(keyRem, 22)
               = bit at position 22
               = 1 (from "10", first bit is 1)
        
        newLeafEdge = extractBits(keyRem, 23, 24-23-1)
                    = extractBits(keyRem, 23, 0)
                    = "" (empty)
        
        Because after bit 22 (which is 1), we only have bit 23 left,
        and we extract from position 23 with length 0 → NOTHING!
        
        Wait... let me recalculate:
        keyRem = 11000000.10101000.00000010
        Positions: 0-21 = 11000000.10101000.000000 (22 bits)
                   position 22 = 1 (newBit)
                   position 23 = 0 (last bit)
        
        newLeafEdge = extractBits(keyRem, 23, 1) = "0" ✓
    */
    newBit := getBitFromBytes(keyRem, shared)          // = 1
    newLeafEdge := extractBits(keyRem, shared+1, remaining-shared-1)  // = "0"
    newLeaf := t.nodeBuilder.Build()
    newLeaf.SetEdge(newLeafEdge, remaining-shared-1)   // edge = "0", len = 1
    newLeaf.SetMetadata(meta)                          // "Office"
    newLeaf.SetPrefix(prefix)                          // 192.168.2.0/24
    
    /*
        STEP 7: Place NEW leaf based on NEW BIT
        ---------------------------------------
        newBit = 1 → NEW node goes to RIGHT
        
        FINAL TREE:
        Root: edge = "11000000.10101000.000000" (22 bits)
              metadata = nil (splitter)
              ├─ LEFT: edge = "1", metadata = "Home" (192.168.1.0)
              └─ RIGHT: edge = "0", metadata = "Office" (192.168.2.0)
        
        Path to "Home":  Root("11000000.10101000.000000") + "1" = "11000000.10101000.00000001" ✓
        Path to "Office": Root("11000000.10101000.000000") + "0" = "11000000.10101000.00000010" ✓
    */
    if newBit == 0 {
        n.SetLeft(newLeaf)   // NEW goes LEFT (newBit = 0)
    } else {
        n.SetRight(newLeaf)  // NEW goes RIGHT (newBit = 1)
    }
    return true
}

/*
    ============================================================================
    DESCEND CASE - When ALL edge bits match
    ============================================================================
    
    This happens when shared == edgeLen (all bits in current node match).
    We need to go DEEPER into the tree.
    
    Example: After inserting 192.168.1.0/24, we insert 192.168.1.100/32
    
    Current node: edge = "11000000.10101000.00000001" (24 bits)
                  metadata = "Home"
    New key:      "11000000.10101000.00000001.01100100" (32 bits)
    
    Compare:
    edgeBits: 11000000.10101000.00000001
    key:      11000000.10101000.00000001.01100100
              ^^^^^^^^^^^^^^^^^^^^^^^^^^^^
              ALL 24 bits match!
    
    shared = 24, edgeLen = 24
    shared < edgeLen? 24 < 24 → FALSE!
    
    So we DESCEND to the next level.
    ============================================================================
*/

// Look at the NEXT BIT after the shared prefix
nextBit := getBitFromBytes(keyRem, shared)  // bit at position 24 = 0

var child RadixNode
if nextBit == 0 {
    child = n.Left()   // Try to go LEFT
} else {
    child = n.Right()  // Try to go RIGHT
}

/*
    If child is nil, we've reached a dead end.
    Create a new leaf with the REMAINING bits.
    
    In our example:
    nextBit = 0 → child = n.Left()
    child == nil (no left child yet!)
    
    So we create:
    newDepth = depth + shared + 1 = 0 + 24 + 1 = 25
    newRemaining = keyLen - newDepth = 32 - 25 = 7
    
    newLeaf.edgeBits = extractBits(key, 25, 7)
                     = bits at positions 25-31
                     = "1100100" (rest of the IP)
    newLeaf.metadata = "My PC"
    
    Place it as LEFT child (because nextBit = 0)
    
    FINAL TREE:
    Root: edge = "11000000.10101000.00000001" (24 bits)
          metadata = "Home" (192.168.1.0/24)
          └─ LEFT: edge = "1100100" (7 bits)
              metadata = "My PC" (192.168.1.100/32)
    
    Path to "Home": Root edge = "11000000.10101000.00000001" (24 bits)
    Path to "My PC": Root edge + "1100100" = "11000000.10101000.00000001.1100100" (32 bits) ✓
*/

if child == nil {
    newDepth := depth + shared + 1
    newRemaining := keyLen - newDepth
    if newRemaining < 0 {
        newRemaining = 0
    }
    newLeaf := t.nodeBuilder.Build()
    newLeaf.SetEdge(extractBits(key, newDepth, newRemaining), newRemaining)
    newLeaf.SetMetadata(meta)
    newLeaf.SetPrefix(prefix)

    if nextBit == 0 {
        n.SetLeft(newLeaf)
    } else {
        n.SetRight(newLeaf)
    }
    return true
}

// If child exists, RECURSE! Go deeper into the tree.
return t.insertNode(child, key, keyLen, depth+shared+1, prefix, meta)
```