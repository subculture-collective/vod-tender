import React, { useEffect, useRef, useState } from 'react';

import { getApiBase } from '../lib/api';
const API_BASE_URL = getApiBase();

// Helper: Parse badges string (e.g. "broadcaster/1,subscriber/12") to array of { set, version }
function parseBadges(
    badges?: string | null
): { set: string; version: string }[] {
    if (!badges) return [];
    return badges
        .split(',')
        .map((b) => {
            const [set, version] = b.split('/');
            return { set, version };
        })
        .filter((b) => b.set && b.version);
}

// Helper: Parse emotes string (e.g. "25:0-4,12-16/1902:6-10") to map of emoteId to positions
function parseEmotes(emotes?: string | null): {
    [emoteId: string]: [number, number][];
} {
    if (!emotes) return {};
    const out: { [emoteId: string]: [number, number][] } = {};
    emotes.split('/').forEach((e) => {
        const [id, pos] = e.split(':');
        if (!id || !pos) return;
        out[id] = pos.split(',').map((range) => {
            const [start, end] = range.split('-').map(Number);
            return [start, end];
        });
    });
    return out;
}

// Helper: Render message with emotes replaced by <img>
function renderEmotes(
    message: string,
    emotes: { [emoteId: string]: [number, number][] }
) {
    if (!emotes || Object.keys(emotes).length === 0) return message;
    // Build a list of { start, end, emoteId }
    const ranges: { start: number; end: number; emoteId: string }[] = [];
    Object.entries(emotes).forEach(([emoteId, positions]) => {
        positions.forEach(([start, end]) => {
            ranges.push({ start, end, emoteId });
        });
    });
    ranges.sort((a, b) => a.start - b.start);
    const out: React.ReactNode[] = [];
    let last = 0;
    for (const { start, end, emoteId } of ranges) {
        if (last < start) out.push(message.slice(last, start));
        out.push(
            <img
                key={start}
                src={`https://static-cdn.jtvnw.net/emoticons/v2/${emoteId}/default/dark/1.0`}
                alt={message.slice(start, end + 1)}
                className='inline h-6 align-text-bottom'
                style={{ display: 'inline', verticalAlign: 'bottom' }}
            />
        );
        last = end + 1;
    }
    if (last < message.length) out.push(message.slice(last));
    return out;
}

export interface ChatMessage {
    username: string;
    message: string;
    abs_timestamp: string;
    rel_timestamp: number;
    badges?: string | null;
    emotes?: string | null;
    color?: string | null;
}

interface ChatReplayProps {
    vodId: string;
}

export default function ChatReplay({ vodId }: ChatReplayProps) {
    const [messages, setMessages] = useState<ChatMessage[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [replay, setReplay] = useState(false);
    const [speed, setSpeed] = useState(1.0);
    const [from, setFrom] = useState(0);
    const [live, setLive] = useState(false);
    const sseRef = useRef<EventSource | null>(null);
    const containerRef = useRef<HTMLDivElement>(null);

    useEffect(() => {
        if (!replay) {
            setLoading(true);
            fetch(`${API_BASE_URL}/vods/${vodId}/chat?from=${from}`)
                .then((r) => r.json())
                .then(setMessages)
                .catch((e) => setError(e.message))
                .finally(() => setLoading(false));
            if (sseRef.current) {
                sseRef.current.close();
                sseRef.current = null;
            }
            setLive(false);
            return;
        }
        // SSE replay
        setMessages([]);
        setLoading(false);
        setLive(true);
        const url = `${API_BASE_URL}/vods/${vodId}/chat/stream?from=${from}&speed=${speed}`;
        const es = new EventSource(url);
        sseRef.current = es;
        es.onmessage = (e) => {
            try {
                const msg = JSON.parse(e.data);
                setMessages((prev: ChatMessage[]) => [...prev, msg]);
                // Auto-scroll
                setTimeout(() => {
                    containerRef.current?.scrollTo({
                        top: containerRef.current.scrollHeight,
                        behavior: 'smooth',
                    });
                }, 10);
            } catch {
                console.log(error);
            }
        };
        es.onerror = () => {
            es.close();
            setLive(false);
        };
        return () => {
            es.close();
            setLive(false);
        };
    }, [vodId, replay, speed, from, error]);

    return (
        <div className='bg-gray-100 rounded p-2 mt-6'>
            <div className='flex items-center gap-2 mb-2'>
                <span className='font-semibold'>Chat Replay</span>
                <button
                    className={`px-2 py-1 rounded text-xs ${
                        replay ? 'bg-indigo-600 text-white' : 'bg-white border'
                    }`}
                    onClick={() => setReplay((v: boolean) => !v)}
                >
                    {replay ? 'Live' : 'Static'}
                </button>
                <label htmlFor='chat-speed' className='text-xs ml-2'>Speed:</label>
                <select
                    id='chat-speed'
                    className='text-xs border rounded px-1 py-0.5'
                    value={speed}
                    onChange={(e: React.ChangeEvent<HTMLSelectElement>) =>
                        setSpeed(Number(e.target.value))
                    }
                    disabled={!replay}
                >
                    <option value={0.5}>0.5x</option>
                    <option value={1.0}>1x</option>
                    <option value={2.0}>2x</option>
                    <option value={4.0}>4x</option>
                </select>
                <label htmlFor='chat-from' className='text-xs ml-2'>From:</label>
                <input
                    id='chat-from'
                    type='number'
                    className='text-xs border rounded px-1 py-0.5 w-16'
                    value={from}
                    min={0}
                    onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                        setFrom(Number(e.target.value))
                    }
                />
            </div>
            {loading && (
                <div className='text-xs text-gray-500'>Loading chat...</div>
            )}
            {error && <div className='text-xs text-red-500'>{error}</div>}
            <div
                ref={containerRef}
                className='h-64 overflow-y-auto bg-white rounded border p-2 text-sm font-mono'
                style={{ fontSize: '0.92em' }}
            >
                {messages.map((msg, i) => {
                    const badges = parseBadges(msg.badges);
                    const emotes = parseEmotes(msg.emotes);
                    return (
                        <div
                            key={i}
                            className='mb-1 flex items-center gap-2'
                        >
                            {/* Badges */}
                            <span className='flex gap-0.5 items-center'>
                                {badges.map((b, j) => (
                                    <img
                                        key={j}
                                        src={`https://static-cdn.jtvnw.net/badges/v1/${b.set}${b.version}/1`}
                                        alt={b.set}
                                        className='inline h-5 w-5 align-text-bottom'
                                        title={b.set}
                                    />
                                ))}
                            </span>
                            {/* Username */}
                            <span
                                className='font-bold'
                                style={{ color: msg.color || undefined }}
                                title={msg.badges || undefined}
                            >
                                {msg.username}:
                            </span>
                            {/* Message with emotes */}
                            <span>{renderEmotes(msg.message, emotes)}</span>
                            <span className='ml-auto text-xs text-gray-400'>
                                {msg.rel_timestamp.toFixed(1)}s
                            </span>
                        </div>
                    );
                })}
                {live && <div className='text-xs text-gray-400'>[live]</div>}
            </div>
        </div>
    );
}
