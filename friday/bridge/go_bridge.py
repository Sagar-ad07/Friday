"""
Friday Go Bridge - ctypes interface to Go shared library
Loads friday_lib.dll and provides Python-friendly wrappers for all exported functions
"""
import ctypes
import json
import os
import platform
from typing import Any, Dict, Optional, Union

class GoBridge:
    """Singleton wrapper for Go shared library via ctypes"""
    _instance = None
    _lib = None
    _initialized = False
    
    def __new__(cls):
        if cls._instance is None:
            cls._instance = super(GoBridge, cls).__new__(cls)
            cls._instance._load_library()
        return cls._instance
    
    def _load_library(self):
        """Load the Go shared library based on platform"""
        if self._lib is not None:
            return
            
        # Determine library name and path
        system = platform.system().lower()
        if system == "windows":
            lib_name = "friday_lib.dll"
        elif system == "darwin":
            lib_name = "libfriday_lib.dylib"
        else:  # linux
            lib_name = "libfriday_lib.so"
        
        # Look for library in several places
        possible_paths = [
            os.path.join(os.path.dirname(__file__), "..", "..", "friday_go", "cmd", "friday_lib", lib_name),
            os.path.join(os.path.dirname(__file__), "..", "..", "friday_go", "cmd", "friday_lib", "bin", lib_name),
            os.path.join(os.getcwd(), lib_name),
            lib_name  # fallback to PATH
        ]
        
        lib_path = None
        for path in possible_paths:
            if os.path.exists(path):
                lib_path = path
                break
        
        if lib_path is None:
            raise FileNotFoundError(f"Could not find {lib_name} in any of: {possible_paths}")
        
        try:
            self._lib = ctypes.CDLL(lib_path)
            self._setup_function_signatures()
        except Exception as e:
            raise RuntimeError(f"Failed to load Go library {lib_path}: {e}")
    
    def _setup_function_signatures(self):
        """Define argument and return types for all exported Go functions"""
        lib = self._lib
        
        # Init function
        lib.Init.argtypes = []
        lib.Init.restype = ctypes.c_int
        
        # Tool execution
        lib.ToolExecute.argtypes = [ctypes.c_char_p, ctypes.c_char_p]
        lib.ToolExecute.restype = ctypes.c_char_p
        
        # Trading functions
        lib.TradingStart.argtypes = []
        lib.TradingStart.restype = ctypes.c_char_p
        
        lib.TradingStop.argtypes = []
        lib.TradingStop.restype = ctypes.c_char_p
        
        lib.TradingStatus.argtypes = []
        lib.TradingStatus.restype = ctypes.c_char_p
        
        # Utility functions
        lib.GetTime.argtypes = []
        lib.GetTime.restype = ctypes.c_char_p
        
        lib.SystemInfo.argtypes = []
        lib.SystemInfo.restype = ctypes.c_char_p
        
        lib.ManageFiles.argtypes = [ctypes.c_char_p]
        lib.ManageFiles.restype = ctypes.c_char_p
        
        lib.WebSearch.argtypes = [ctypes.c_char_p]
        lib.WebSearch.restype = ctypes.c_char_p
        
        lib.OpenURL.argtypes = [ctypes.c_char_p]
        lib.OpenURL.restype = ctypes.c_char_p
        
        lib.Calc.argtypes = [ctypes.c_char_p]
        lib.Calc.restype = ctypes.c_char_p
        
        lib.Remember.argtypes = [ctypes.c_char_p]
        lib.Remember.restype = ctypes.c_char_p
        
        lib.Recall.argtypes = [ctypes.c_char_p]
        lib.Recall.restype = ctypes.c_char_p
    
    def _init_if_needed(self):
        """Initialize the library if not already done"""
        if not self._initialized:
            result = self._lib.Init()
            if result != 1:
                raise RuntimeError("Failed to initialize Go library")
            self._initialized = True
    
    def _call_str(self, func, *args) -> str:
        """Call a function that returns a string and decode it"""
        self._init_if_needed()
        result = func(*args)
        if not result:
            return ""
        return ctypes.string_at(result).decode('utf-8')
    
    def _call_int(self, func, *args) -> int:
        """Call a function that returns an integer"""
        self._init_if_needed()
        return func(*args)
    
    # Public API methods
    def tool_execute(self, tool: str, args: dict) -> str:
        """Execute a tool with given arguments"""
        if not tool:
            return "error: tool name required"
        
        try:
            args_json = json.dumps(args)
        except (TypeError, ValueError) as e:
            return f"error: invalid args JSON: {e}"
        
        result = self._call_str(
            self._lib.ToolExecute,
            ctypes.c_char_p(tool.encode('utf-8')),
            ctypes.c_char_p(args_json.encode('utf-8'))
        )
        return result
    
    def trading_start(self) -> str:
        """Start the trading bot"""
        result = self._call_str(self._lib.TradingStart)
        if result.startswith("error:"):
            return result
        return result
    
    def trading_stop(self) -> str:
        """Stop the trading bot"""
        result = self._call_str(self._lib.TradingStop)
        return result
    
    def trading_status(self) -> dict:
        """Get trading status as dict"""
        result = self._call_str(self._lib.TradingStatus)
        try:
            return json.loads(result)
        except (json.JSONDecodeError, TypeError):
            return {"error": "invalid status response", "raw": result}
    
    def get_time(self) -> str:
        """Get current time"""
        return self._call_str(self._lib.GetTime)
    
    def system_info(self) -> str:
        """Get system information"""
        return self._call_str(self._lib.SystemInfo)
    
    def manage_files(self, action: dict) -> str:
        """Manage files (list, read, write, create, delete)"""
        try:
            args_json = json.dumps(action)
        except (TypeError, ValueError) as e:
            return f"error: invalid action JSON: {e}"
        
        result = self._call_str(
            self._lib.ManageFiles,
            ctypes.c_char_p(args_json.encode('utf-8'))
        )
        return result
    
    def web_search(self, query: str) -> str:
        """Perform web search"""
        if not query:
            return "error: query required"
        
        result = self._call_str(
            self._lib.WebSearch,
            ctypes.c_char_p(query.encode('utf-8'))
        )
        return result
    
    def open_url(self, url: str) -> str:
        """Open and fetch URL content"""
        if not url:
            return "error: URL required"
        
        result = self._call_str(
            self._lib.OpenURL,
            ctypes.c_char_p(url.encode('utf-8'))
        )
        return result
    
    def calc(self, expression: str) -> str:
        """Calculate mathematical expression"""
        if not expression:
            return "error: expression required"
        
        result = self._call_str(
            self._lib.Calc,
            ctypes.c_char_p(expression.encode('utf-8'))
        )
        return result
    
    def remember_fact(self, fact: str) -> str:
        """Remember a fact"""
        if not fact:
            return "error: fact required"
        
        result = self._call_str(
            self._lib.Remember,
            ctypes.c_char_p(fact.encode('utf-8'))
        )
        return result
    
    def recall_facts(self, query: str = "") -> list:
        """Recall facts matching query"""
        result = self._call_str(
            self._lib.Recall,
            ctypes.c_char_p(query.encode('utf-8'))
        )
        try:
            return json.loads(result)
        except (json.JSONDecodeError, TypeError):
            # Return as list if possible
            if result.startswith('[') and result.endswith(']'):
                try:
                    return json.loads(result)
                except:
                    return [result] if result else []
            return [result] if result else []

# Singleton instance
_go_bridge = None

def get_go_bridge() -> GoBridge:
    """Get the singleton GoBridge instance"""
    global _go_bridge
    if _go_bridge is None:
        _go_bridge = GoBridge()
    return _go_bridge

# Convenience functions for direct use
def tool_execute(tool: str, args: dict) -> str:
    """Execute a tool via Go library"""
    return get_go_bridge().tool_execute(tool, args)

def trading_start() -> str:
    """Start trading bot"""
    return get_go_bridge().trading_start()

def trading_stop() -> str:
    """Stop trading bot"""
    return get_go_bridge().trading_stop()

def trading_status() -> dict:
    """Get trading status"""
    return get_go_bridge().trading_status()

def get_time() -> str:
    """Get current time"""
    return get_go_bridge().get_time()

def system_info() -> str:
    """Get system info"""
    return get_go_bridge().system_info()

def manage_files(action: dict) -> str:
    """Manage files"""
    return get_go_bridge().manage_files(action)

def web_search(query: str) -> str:
    """Web search"""
    return get_go_bridge().web_search(query)

def open_url(url: str) -> str:
    """Open URL"""
    return get_go_bridge().open_url(url)

def calc(expression: str) -> str:
    """Calculate expression"""
    return get_go_bridge().calc(expression)

def remember_fact(fact: str) -> str:
    """Remember a fact"""
    return get_go_bridge().remember_fact(fact)

def recall_facts(query: str = "") -> list:
    """Recall facts"""
    return get_go_bridge().recall_facts(query)