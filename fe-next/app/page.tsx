'use client';

import React, { useState, useRef, useEffect } from 'react';

// Define the structure of a single message object
interface ChatMessage {
    role: 'user' | 'ai'; // Explicitly define the allowed roles
    text: string;
}

interface BackendResponse {
    text: string;
}

export default function Home() {
  //const [chatHistory, setChatHistory] = useState([
  //  { role: 'ai', text: 'Hello! How can I assist you today?' },
  //]);
  // Start with an empty array. The history will be loaded/restored in useEffect.
  //const [chatHistory, setChatHistory] = useState<any[]>([]); 
  // Use the defined interface
  const [chatHistory, setChatHistory] = useState<ChatMessage[]>([]);
    
  // NEW state to track if we're done loading the history
  const [isHistoryLoaded, setIsHistoryLoaded] = useState(false);

  const [userInput, setUserInput] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const chatHistoryRef = useRef<HTMLDivElement>(null);
  const sessionIdRef = useRef<string | null>(null);

  const fetchHistory = async (sessionId: string) => {
	try {
		// Ensure the backend URL matches what's used in your fetch call
		const backendUrl = 'http://localhost:8080'; 
		
		const response = await fetch(`${backendUrl}/chat/history?sessionId=${sessionId}`);
		
		if (response.ok) {
			const history = await response.json();
			
			// 6. Initialize the main chat history state
			// The history can be [] for a new session or a list of messages for a restored session
			setChatHistory(history); 
			
			// console.log('History loaded:', history);
		} else {
			console.error("Failed to load chat history:", response.status);
			// Handle error gracefully if needed
		}
	} catch (error) {
		console.error("Error communicating with history endpoint:", error);
	}
	
	// Always set history loaded when fetch resolves (success or failure)
    setIsHistoryLoaded(true);
  };

  // Function to scroll the chat history to the bottom
  const scrollToBottom = () => {
    if (chatHistoryRef.current) {
      chatHistoryRef.current.scrollTop = chatHistoryRef.current.scrollHeight;
    }
  };

  
  // ----------------------------------------------------------------
  // EFFECT 1 (Initialization and History Loading) - Runs ONLY on mount
  // ----------------------------------------------------------------
  useEffect(() => {
    let currentSessionId = localStorage.getItem('chatSessionId');
    const isNewSession = !currentSessionId;

    if (isNewSession) {
      currentSessionId = crypto.randomUUID(); 
      localStorage.setItem('chatSessionId', currentSessionId);
      console.log('Generated New Session ID:', currentSessionId);
      
      setChatHistory([{ 
		role: 'ai', 
		text: 'Hello! How can I assist you today?' 
	  } as ChatMessage ]);

      // For a NEW session, manually add the welcome message to the state
      // and mark history as loaded immediately.
      setChatHistory([{ role: 'ai', text: 'Hello! How can I assist you today?' }]); 
      setIsHistoryLoaded(true); // <-- Mark loaded

      // If new session, we don't fetch history, we start with the default message.
      // If you want the chat to start empty: setChatHistory([]);

      // We'll keep your default message for now, but in a true stateless FE, 
      // the initial state should be set by the history fetch. Let's start empty:
      //setChatHistory([]); // Start empty since we are managing state on BE
    } else {
      console.log('Restoring Existing Session ID:', currentSessionId);
      // Fetch history only if a persistent ID exists
      // Use a non-null assertion operator (the '!') to tell TypeScript it's safe.
      // We know it's not null because we passed the 'if (!currentSessionId)' check.
      fetchHistory(currentSessionId!); 
    }

    sessionIdRef.current = currentSessionId;
  }, []); // Dependency array is EMPTY: runs only on mount

  // ----------------------------------------------------------------
  // EFFECT 2 (UI Side Effect) - Runs when history or loading state changes
  // ----------------------------------------------------------------
  // Automatically scroll to the bottom whenever chatHistory or isLoading changes
  useEffect(() => {
    scrollToBottom();
  }, [chatHistory, isLoading]);

  // Function to send a message to the LLM
  const sendMessage = async () => {
    if (userInput.trim() === '') {
      return;
    }
    // Add this check at the start of sendMessage:
	if (!sessionIdRef.current) {
		console.error("Session ID not initialized.");
		// This should theoretically not happen if the useEffect ran, but is good safety.
		return;
	}

    // NOTE: This line is still used for optimistic rendering on the FE
    const userMessage: ChatMessage = { role: 'user', text: userInput.trim() }; // <-- Use ChatMessage
    const newChatHistory = [...chatHistory, userMessage];
    setChatHistory(newChatHistory);

    setUserInput('');
    setIsLoading(true);

    try {
      // 1. Safety Check (if you haven't added it already)
      if (!sessionIdRef.current) {
        throw new Error("Session ID is missing, cannot send message.");
      }

      const payload = {
        // 1. ADD THE SESSION ID
        sessionId: sessionIdRef.current, // Should be typed as string
        modelName: "gemini",
        // 2. ONLY SEND THE NEW USER MESSAGE (history management moves to BE)
        contents: [{ 
            role: 'user', 
            text: userInput.trim() 
        }],
        //contents: newChatHistory.map((msg) => ({
        //  role: msg.role === 'user' ? 'user' : 'model',
        //  text: msg.text,
        //})),
      };

      // Now, we call the local Go backend instead of the Gemini API directly
      const backendUrl = 'http://localhost:8080/chat';
      const response = await fetch(backendUrl, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });

      if (!response.ok) {
        const errorData = await response.json() as { text: string };
        //const errorData = await response.json();
        throw new Error(`Server error: ${response.status} - ${errorData.text}`);
      }

      //const result = await response.json();
      // Cast the result to the expected type
	  const result = await response.json() as BackendResponse;

      if (result.text) {
        // FIX 1: Explicitly define the type of the new message
		const aiMessage: ChatMessage = { 
		  role: 'ai', 
		  text: result.text as string // FIX 2: Explicitly cast result.text as string
		};
        //const aiMessage = { role: 'ai', text: result.text };
        //setChatHistory((currentHistory) => [...currentHistory, aiMessage]);
        // FIX 3: Ensure the function we pass to setChatHistory returns ChatMessage[]
		setChatHistory((currentHistory): ChatMessage[] => [
			...currentHistory, 
			aiMessage
		]);
      } else {
        // Do the same for the error message object
		const errorMessage: ChatMessage = {
		  role: 'ai',
		  text: "Sorry, I couldn't get a response from the AI.",
		};
        //const errorMessage = {
        //  role: 'ai',
        //  text: "Sorry, I couldn't get a response from the AI.",
        //};
        setChatHistory((currentHistory) => [...currentHistory, errorMessage]);
        console.error('Unexpected backend response structure:', result);
      }
    } catch (error) {
	  //Use a type guard to check if 'error' is an Error object
      let message = 'An unknown error occurred.';

      if (error instanceof Error) {
          // Now TypeScript knows 'error' has the '.message' property
          message = error.message;
      } else if (typeof error === 'string') {
          // Handle cases where the error might be a plain string
          message = error;
      }

      //const errorMessage = {
      //  role: 'ai',
      //  text: 'An error occurred: ' + message,
      //};
      // Explicitly type the errorMessage object
	  const errorMessage: ChatMessage = { // <-- FIX: Define the type here
		role: 'ai',
		text: 'An error occurred: ' + message,
	  };
      setChatHistory((currentHistory) => [...currentHistory, errorMessage]);
      console.error('Error communicating with Go backend:', error);
    } finally {
      setIsLoading(false);
    }
  };

  //Explicitly define the type as React.KeyboardEvent<HTMLInputElement>
  const handleKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Enter') {
      sendMessage();
    }
  };

  return (
    <main className="font-sans bg-[#f0f2f5] flex justify-center items-center min-h-screen m-0">
		{/* Conditional Rendering Block */}
        {!isHistoryLoaded ? (
            <div className="flex justify-center items-center w-full min-h-[500px]">
                <p className="text-gray-500 animate-pulse">Loading conversation...</p>
            </div>
        ) : (
			<div className="bg-white rounded-[1rem] shadow-lg w-[90%] max-w-[600px] flex flex-col overflow-hidden min-h-[500px] max-h-[90vh]">
				<div ref={chatHistoryRef} className="grow p-6 overflow-y-auto border-b border-solid border-gray-200 flex flex-col gap-3">
					{chatHistory.map((message, index) => (
					  <div key={index} className={`max-w-[80%] py-3 px-4 rounded-xl break-words ${
						message.role === 'user'
						? 'bg-[#3b82f6] text-white self-end'
						: 'bg-[#e2e8f0] text-[#333333] self-start'
					  }`}>
						{message.text}
					  </div>
					))}
					
					{isLoading && (
					  <div className="loading-indicator self-start py-3 px-4 rounded-xl bg-[#e2e8f0] text-[#333333] italic opacity-80 animate-pulse">
						AI is thinking...
					  </div>
					)}
				</div>
				
				<div className="flex p-6 gap-3 items-center">
					<input type="text" id="user-input" className="grow py-3 px-4 border-[1px] border-solid border-[#cbd5e1] rounded-xl outline-none text-base text-black focus:border-[#3b82f6] focus:shadow-[0_0_0_2px_rgba(59,130,246,0.25)]" placeholder="Type your message..." value={userInput}
					  onChange={(e) => setUserInput(e.target.value)}
					  onKeyDown={handleKeyDown}
					  disabled={isLoading}></input>
					<button
					  onClick={sendMessage}
					  className={`bg-[#3b82f6] text-white py-3 px-5 rounded-xl transition-colors duration-200 ease-in-out font-semibold border-none ${
						isLoading
						? 'opacity-50 cursor-not-allowed'
						: 'hover:bg-[#2563eb] cursor-pointer'
					  }`}
					  disabled={isLoading}
					>Send</button>
				</div>
			</div>
		)}
    </main>
  );
}
