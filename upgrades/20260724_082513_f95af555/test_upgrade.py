import sys
import unittest
from friday.advanced_response import craft_sophisticated_response

class TestAdvancedResponse(unittest.TestCase):
    def test_trading_response(self):
        response = craft_sophisticated_response("trading bot status", {
            "bot_id": "eurusd_5k_orb",
            "current_profit": 182.50,
            "target_profit": 250.0,
            "remaining": 67.50
        })
        self.assertIn("TRADING BOT STATUS", response)
        self.assertIn("Bot: eurusd_5k_orb", response)
        self.assertIn("Strategy: ORB (Open Range Breakout)", response)

    def test_completed_bot(self):
        response = craft_sophisticated_response("trading bot completed", {
            "bot_id": "eurusd_5k_orb",
            "current_profit": 254.0,
            "target_profit": 250.0
        })
        self.assertIn("TARGET ACHIEVED", response)
        self.assertIn("Bot: eurusd_5k_orb", response)
        self.assertIn("Status: ✅ Complete", response)

if __name__ == '__main__':
    sys.path.insert(0, '../')
    unittest.main()
    print('UPGRADE TEST PASSED')