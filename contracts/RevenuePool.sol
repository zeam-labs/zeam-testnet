// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/**
 * @title ZeamRevenuePool
 * @notice Minimal on-chain pool for distributing arbitrage revenue to storage providers.
 *
 * Revenue flow:
 * 1. Arbitrage profits are sent to this contract via receive()
 * 2. At epoch end, operator submits merkle root of (address, amount) pairs
 * 3. Users claim their share by providing merkle proofs
 *
 * This is the only on-chain component. All reward calculation happens off-chain
 * using the middle-out economic curves.
 */
contract ZeamRevenuePool {
    address public operator;

    // epoch => merkle root of (address, amount) pairs
    mapping(uint256 => bytes32) public epochRoots;

    // epoch => total amount distributed
    mapping(uint256 => uint256) public epochTotals;

    // epoch => address => claimed
    mapping(uint256 => mapping(address => bool)) public claimed;

    // Total accumulated but undistributed revenue
    uint256 public pendingRevenue;

    // Current epoch number
    uint256 public currentEpoch;

    // Events
    event RevenueReceived(address indexed from, uint256 amount);
    event EpochFinalized(uint256 indexed epoch, bytes32 root, uint256 totalDistributed);
    event Claimed(uint256 indexed epoch, address indexed recipient, uint256 amount);
    event OperatorUpdated(address indexed oldOperator, address indexed newOperator);

    modifier onlyOperator() {
        require(msg.sender == operator, "not operator");
        _;
    }

    constructor() {
        operator = msg.sender;
    }

    /**
     * @notice Receive arbitrage profits.
     */
    receive() external payable {
        pendingRevenue += msg.value;
        emit RevenueReceived(msg.sender, msg.value);
    }

    /**
     * @notice Operator submits merkle root at epoch end.
     * @param root Merkle root of (address, amount) leaves
     * @param totalDistributed Total wei being distributed this epoch
     */
    function finalizeEpoch(bytes32 root, uint256 totalDistributed) external onlyOperator {
        require(root != bytes32(0), "empty root");
        require(totalDistributed <= pendingRevenue, "insufficient funds");
        require(epochRoots[currentEpoch] == bytes32(0), "epoch already finalized");

        epochRoots[currentEpoch] = root;
        epochTotals[currentEpoch] = totalDistributed;
        pendingRevenue -= totalDistributed;

        emit EpochFinalized(currentEpoch, root, totalDistributed);
        currentEpoch++;
    }

    /**
     * @notice Claim rewards for an epoch using merkle proof.
     * @param epoch Epoch number to claim
     * @param amount Amount to claim (must match merkle leaf)
     * @param proof Merkle proof path
     */
    function claim(
        uint256 epoch,
        uint256 amount,
        bytes32[] calldata proof
    ) external {
        require(!claimed[epoch][msg.sender], "already claimed");
        require(epochRoots[epoch] != bytes32(0), "epoch not finalized");
        require(amount > 0, "zero amount");

        // Compute leaf hash
        bytes32 leaf = keccak256(abi.encodePacked(msg.sender, amount));

        // Verify merkle proof
        require(_verify(proof, epochRoots[epoch], leaf), "invalid proof");

        claimed[epoch][msg.sender] = true;

        (bool success, ) = payable(msg.sender).call{value: amount}("");
        require(success, "transfer failed");

        emit Claimed(epoch, msg.sender, amount);
    }

    /**
     * @notice Check if an address can claim from an epoch.
     */
    function canClaim(
        uint256 epoch,
        address account,
        uint256 amount,
        bytes32[] calldata proof
    ) external view returns (bool) {
        if (claimed[epoch][account]) return false;
        if (epochRoots[epoch] == bytes32(0)) return false;

        bytes32 leaf = keccak256(abi.encodePacked(account, amount));
        return _verify(proof, epochRoots[epoch], leaf);
    }

    /**
     * @notice Update operator address.
     */
    function setOperator(address newOperator) external onlyOperator {
        require(newOperator != address(0), "zero address");
        emit OperatorUpdated(operator, newOperator);
        operator = newOperator;
    }

    /**
     * @notice Verify a merkle proof.
     */
    function _verify(
        bytes32[] calldata proof,
        bytes32 root,
        bytes32 leaf
    ) internal pure returns (bool) {
        bytes32 hash = leaf;

        for (uint256 i = 0; i < proof.length; i++) {
            bytes32 proofElement = proof[i];

            // Sort to ensure consistent hashing
            if (hash < proofElement) {
                hash = keccak256(abi.encodePacked(hash, proofElement));
            } else {
                hash = keccak256(abi.encodePacked(proofElement, hash));
            }
        }

        return hash == root;
    }

    /**
     * @notice Get contract stats.
     */
    function getStats() external view returns (
        uint256 _currentEpoch,
        uint256 _pendingRevenue,
        uint256 _contractBalance
    ) {
        return (currentEpoch, pendingRevenue, address(this).balance);
    }
}
