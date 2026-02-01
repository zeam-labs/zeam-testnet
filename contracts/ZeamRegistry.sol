// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/**
 * @title ZeamRegistry
 * @notice Minimal registry for zeam update manifests.
 *
 * Stores a content-addressed hash pointing to the latest manifest.
 * Nodes query this on startup and periodically to check for updates.
 * Manifest contains per-platform binary hashes for verification.
 */
contract ZeamRegistry {
    address public owner;
    bytes32 public manifestHash;
    uint256 public lastUpdated;

    event ManifestUpdated(bytes32 indexed oldHash, bytes32 indexed newHash, uint256 timestamp);
    event OwnershipTransferred(address indexed oldOwner, address indexed newOwner);

    modifier onlyOwner() {
        require(msg.sender == owner, "not owner");
        _;
    }

    constructor() {
        owner = msg.sender;
    }

    /**
     * @notice Update the manifest hash.
     * @param newHash Content-addressed hash of the manifest JSON
     */
    function setManifest(bytes32 newHash) external onlyOwner {
        require(newHash != bytes32(0), "empty hash");
        bytes32 oldHash = manifestHash;
        manifestHash = newHash;
        lastUpdated = block.timestamp;
        emit ManifestUpdated(oldHash, newHash, block.timestamp);
    }

    /**
     * @notice Transfer ownership to a new address.
     */
    function transferOwnership(address newOwner) external onlyOwner {
        require(newOwner != address(0), "zero address");
        emit OwnershipTransferred(owner, newOwner);
        owner = newOwner;
    }

    /**
     * @notice Get registry info.
     */
    function getInfo() external view returns (
        bytes32 _manifestHash,
        uint256 _lastUpdated,
        address _owner
    ) {
        return (manifestHash, lastUpdated, owner);
    }
}
