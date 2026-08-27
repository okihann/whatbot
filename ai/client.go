package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
	"bot/types"
)

async function handleChat(request, env) {
  const rate = await checkRateLimit(request, env);
  if (!rate.allowed) return jsonResponse({ error: 'Too many requests' }, 429);

  const auth = await requireAuth(request, env);
  if (auth.error) return jsonResponse({ error: auth.error }, 401);

  try {
    const {
      messages,
      model,
      customBaseUrl,
      customApiKey,
      webSearchEnabled,
      thinkEnabled,
      mcpServerUrl,
      mcpApiKey,
    } = await request.json();

    if (!customBaseUrl) return jsonResponse({ error: 'Missing Base URL' }, 400);

    // Fetch MCP tools once
    let mcpTools = [];
    if (mcpServerUrl) {
      try {
        mcpTools = await listMCPServerTools(mcpServerUrl, mcpApiKey);
      } catch (e) {
        console.error('MCP list error:', e);
        return jsonResponse({ error: `MCP server error: ${e.message}` }, 502);
      }
    }

    const endpoint = `${customBaseUrl.replace(/\/$/, '')}/chat/completions`;
    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), 58000);

    let currentMessages = messages; // Start with original messages
    let aiMessage;

    // Tool call loop (max 5 iterations)
    for (let i = 0; i < 5; i++) {
      const payload = {
        model: model || 'anyapi/llama-3.1-8b-instant',
        messages: currentMessages,
        webSearchEnabled: !!webSearchEnabled,
        thinkEnabled: !!thinkEnabled,
        stream: false,
      };

      if (mcpTools.length > 0) {
        payload.tools = mcpTools;
        payload.tool_choice = 'auto';
      }

      const resp = await fetch(endpoint, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${customApiKey || ''}`,
          'ngrok-skip-browser-warning': 'true',
        },
        body: JSON.stringify(payload),
        signal: controller.signal,
      });
      clearTimeout(timeoutId);

      if (!resp.ok) {
        const errText = await resp.text();
        return jsonResponse({ error: `Backend returned ${resp.status}: ${errText}` }, resp.status);
      }

      const data = await resp.json();
      aiMessage = data.choices?.[0]?.message;

      // If no tool calls, we're done
      if (!aiMessage?.tool_calls?.length) break;

      // Add assistant message with tool calls to currentMessages
      currentMessages.push(aiMessage);

      // Execute each MCP tool and collect results
      const toolResults = [];
      for (const toolCall of aiMessage.tool_calls) {
        const toolName = toolCall.function.name;
        const toolArgs = JSON.parse(toolCall.function.arguments || '{}');
        try {
          const result = await executeMCPTool(mcpServerUrl, mcpApiKey, toolName, toolArgs);
          toolResults.push({
            tool_call_id: toolCall.id,
            role: 'tool',
            content: JSON.stringify(result),
          });
        } catch (e) {
          toolResults.push({
            tool_call_id: toolCall.id,
            role: 'tool',
            content: `Error: ${e.message}`,
          });
        }
      }

      // Append tool results
      currentMessages.push(...toolResults);
    }

    // If we exited loop due to max iterations without final answer, use last aiMessage content
    const aiText = aiMessage?.content || 'No response.';

    // Stream final content back
    const encoder = new TextEncoder();
    const stream = new ReadableStream({
      start(ctrl) {
        ctrl.enqueue(encoder.encode(`0:${JSON.stringify(aiText)}\n`));
        ctrl.close();
      },
    });

    return new Response(stream, {
      headers: {
        'Content-Type': 'text/plain; charset=utf-8',
        'x-vercel-ai-data-stream': 'v1',
        ...corsHeaders,
        ...securityHeaders,
      },
    });
  } catch (e) {
    console.error('Chat handler error:', e);
    if (e.name === 'AbortError') return jsonResponse({ error: 'Timeout' }, 504);
    return jsonResponse({ error: e.message }, 500);
  }
}
			
