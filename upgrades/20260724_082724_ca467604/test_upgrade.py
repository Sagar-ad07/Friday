import ast
import os

# Insert the project root onto sys.path
import sys
sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), '..')))

# Import the candidate
from friday.advanced_response import craft_sophisticated_response

# Test the new behavior
try:
    # Test the trading bot status response
    response = craft_sophisticated_response("trading bot status", {
        "bot_id": "eurusd_5k_orb",
        "current_profit": 182.50,
        "target_profit": 250.0,
        "remaining": 67.50
    })
    assert "TRADING BOT STATUS" in response
    
    # Test the completed bot response
    response = craft_sophisticated_response("trading bot completed", {
        "bot_id": "eurusd_5k_orb",
        "current_profit": 254.0,
        "target_profit": 250.0
    })
    assert "TARGET ACHIEVED" in response
    
    print("UPGRADE TEST PASSED")
except AssertionError:
    print("UPGRADE TEST FAILED")
    sys.exit(1)