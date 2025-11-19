'use client';

import React, { useState, useRef, useEffect } from 'react';

export default function Home() {
  const [chatHistory, setChatHistory] = useState([
    { role: 'ai', text: 'Hello! How can I assist you today?' },
  ]);
  const [userInput, setUserInput] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const chatHistoryRef = useRef<HTMLDivElement>(null);

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

    const userMessage = { role: 'user', text: userInput.trim() };
    const newChatHistory = [...chatHistory, userMessage];

    setChatHistory(newChatHistory);
    setUserInput('');
    setIsLoading(true);

    try {
      const payload = {
        modelName: "gemini",
        contents: newChatHistory.map((msg) => ({
          role: msg.role === 'user' ? 'user' : 'model',
          text: msg.text,
        })),
        generationConfig: {
          temperature: 0.7,
          topP: 0.95,
          topK: 40,
          maxOutputTokens: 1024,
        },
      };

      // Now, we call the local Go backend instead of the Gemini API directly
      const backendUrl = 'http://localhost:8080/chat';
      const response = await fetch(backendUrl, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });

      if (!response.ok) {
        const errorData = await response.json();
        throw new Error(`Server error: ${response.status} - ${errorData.text}`);
      }

      const result = await response.json();

      if (result.text) {
        const aiMessage = { role: 'ai', text: result.text };
        setChatHistory((currentHistory) => [...currentHistory, aiMessage]);
      } else {
        const errorMessage = {
          role: 'ai',
          text: "Sorry, I couldn't get a response from the AI.",
        };
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

      const errorMessage = {
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
