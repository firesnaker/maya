'use client';

import { useState, useRef, useEffect } from 'react';

export default function Home() {
  const [chatHistory, setChatHistory] = useState([
    { role: 'ai', text: 'Hello! How can I assist you today?' },
  ]);
  const [userInput, setUserInput] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const chatHistoryRef = useRef(null);
  
  // The API key is now loaded from the environment
  const apiKey = process.env.NEXT_PUBLIC_GEMINI_API_KEY;

  // Function to scroll the chat history to the bottom
  const scrollToBottom = () => {
    if (chatHistoryRef.current) {
      chatHistoryRef.current.scrollTop = chatHistoryRef.current.scrollHeight;
    }
  };

  // Automatically scroll to the bottom whenever chatHistory or isLoading changes
  useEffect(() => {
    scrollToBottom();
  }, [chatHistory, isLoading]);

  // Function to send a message to the LLM
  const sendMessage = async () => {
    if (userInput.trim() === '') {
      return;
    }
    
    // Check if the API key is available
    if (!apiKey) {
      console.error("API key is not configured. Please set NEXT_PUBLIC_GEMINI_API_KEY in your .env.local file.");
      return;
    }

    const userMessage = { role: 'user', text: userInput.trim() };
    const newChatHistory = [...chatHistory, userMessage];

    setChatHistory(newChatHistory);
    setUserInput('');
    setIsLoading(true);

    try {
      const payload = {
        contents: newChatHistory.map((msg) => ({
          role: msg.role === 'user' ? 'user' : 'model',
          parts: [{ text: msg.text }],
        })),
        generationConfig: {
          temperature: 0.7,
          topP: 0.95,
          topK: 40,
          maxOutputTokens: 1024,
        },
      };

      //Use the environment variable here
      const apiUrl = `https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent?key=${apiKey}`;

      const response = await fetch(apiUrl, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });

      if (!response.ok) {
        const errorData = await response.json();
        throw new Error(
          `API error: ${response.status} - ${errorData.error.message}`
        );
      }

      const result = await response.json();

      if (
        result.candidates &&
        result.candidates.length > 0 &&
        result.candidates[0].content &&
        result.candidates[0].content.parts &&
        result.candidates[0].content.parts.length > 0
      ) {
        const aiText = result.candidates[0].content.parts[0].text;
        const aiMessage = { role: 'ai', text: aiText };
        setChatHistory((currentHistory) => [...currentHistory, aiMessage]);
      } else {
        const errorMessage = {
          role: 'ai',
          text: "Sorry, I couldn't get a response from the AI.",
        };
        setChatHistory((currentHistory) => [...currentHistory, errorMessage]);
        console.error('Unexpected API response structure:', result);
      }
    } catch (error) {
      const errorMessage = {
        role: 'ai',
        text: 'An error occurred: ' + error.message,
      };
      setChatHistory((currentHistory) => [...currentHistory, errorMessage]);
      console.error('Error communicating with LLM:', error);
    } finally {
      setIsLoading(false);
    }
  };

  const handleKeyDown = (event) => {
    if (event.key === 'Enter') {
      sendMessage();
    }
  };

  return (
    <main className="font-sans bg-[#f0f2f5] flex justify-center items-center min-h-screen m-0">
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
    </main>
  );
}
