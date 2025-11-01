-- Seed data for local development
-- This file provides sample VODs, chat messages, and configuration for testing
-- Run with: psql -U vod -d vod -f backend/db/seed-dev-data.sql
-- Or use: make seed

BEGIN;

-- Clear existing seed data (keep real data if any)
DELETE FROM chat_messages WHERE vod_id LIKE 'seed-%';
DELETE FROM vods WHERE twitch_vod_id LIKE 'seed-%';
DELETE FROM kv WHERE key LIKE 'seed_%';

-- Insert sample VODs with various states for testing
INSERT INTO vods (
    twitch_vod_id, 
    title, 
    date, 
    duration_seconds, 
    downloaded_path, 
    download_state, 
    download_retries,
    download_bytes,
    download_total,
    processed, 
    processing_error,
    youtube_url,
    description,
    priority,
    channel,
    created_at,
    updated_at
) VALUES
    -- Completed VOD with chat
    (
        'seed-completed-001',
        'Epic Gameplay Session - Boss Fight Marathon',
        NOW() - INTERVAL '2 days',
        7234,  -- ~2 hours
        '/data/seed-completed-001.mp4',
        'completed',
        0,
        1500000000,  -- 1.5 GB
        1500000000,
        true,
        NULL,
        'https://youtube.com/watch?v=seed001',
        'An amazing gaming session with chat participation',
        0,
        '',
        NOW() - INTERVAL '2 days',
        NOW() - INTERVAL '1 day'
    ),
    -- In-progress download
    (
        'seed-downloading-002',
        'Late Night Stream - Viewer Requests',
        NOW() - INTERVAL '1 day',
        5430,  -- ~1.5 hours
        '/data/seed-downloading-002.part',
        'downloading',
        1,
        750000000,   -- 750 MB downloaded
        1200000000,  -- 1.2 GB total (62.5% complete)
        false,
        NULL,
        NULL,
        'Community-driven gameplay stream',
        5,
        '',
        NOW() - INTERVAL '1 day',
        NOW() - INTERVAL '1 hour'
    ),
    -- Pending VOD (not yet processed)
    (
        'seed-pending-003',
        'Tournament Practice - Day 1',
        NOW() - INTERVAL '6 hours',
        9876,  -- ~2.75 hours
        NULL,
        'pending',
        0,
        0,
        0,
        false,
        NULL,
        NULL,
        'Professional tournament preparation stream',
        10,
        '',
        NOW() - INTERVAL '6 hours',
        NOW() - INTERVAL '6 hours'
    ),
    -- Failed VOD with error
    (
        'seed-failed-004',
        'Speedrun Attempts - World Record Chase',
        NOW() - INTERVAL '12 hours',
        3245,  -- ~54 minutes
        NULL,
        'failed',
        3,
        0,
        0,
        false,
        'Download error: network timeout after 3 retries',
        NULL,
        'Multiple speedrun attempts with commentary',
        0,
        '',
        NOW() - INTERVAL '12 hours',
        NOW() - INTERVAL '2 hours'
    ),
    -- High priority VOD
    (
        'seed-priority-005',
        'Special Event - Community Celebration',
        NOW() - INTERVAL '3 hours',
        4567,  -- ~1.25 hours
        NULL,
        'pending',
        0,
        0,
        0,
        false,
        NULL,
        NULL,
        'Special community milestone celebration stream',
        100,
        '',
        NOW() - INTERVAL '3 hours',
        NOW() - INTERVAL '3 hours'
    ),
    -- Completed VOD without YouTube upload
    (
        'seed-completed-006',
        'Chill Stream - Chatting and Gaming',
        NOW() - INTERVAL '5 days',
        8901,  -- ~2.5 hours
        '/data/seed-completed-006.mp4',
        'completed',
        0,
        1800000000,  -- 1.8 GB
        1800000000,
        true,
        NULL,
        NULL,
        'Casual stream with community interaction',
        0,
        '',
        NOW() - INTERVAL '5 days',
        NOW() - INTERVAL '4 days'
    ),
    -- Very old VOD
    (
        'seed-archive-007',
        'Classic Stream Archive - First Ever Stream',
        NOW() - INTERVAL '90 days',
        6543,  -- ~1.8 hours
        '/data/seed-archive-007.mp4',
        'completed',
        0,
        1300000000,  -- 1.3 GB
        1300000000,
        true,
        NULL,
        'https://youtube.com/watch?v=seed007',
        'Historical archive from channel beginnings',
        0,
        '',
        NOW() - INTERVAL '90 days',
        NOW() - INTERVAL '89 days'
    );

-- Insert sample chat messages for the completed VOD (seed-completed-001)
INSERT INTO chat_messages (
    vod_id,
    username,
    message,
    abs_timestamp,
    rel_timestamp,
    badges,
    emotes,
    color,
    reply_to_id,
    reply_to_username,
    reply_to_message,
    channel,
    created_at
) VALUES
    -- Chat messages at various points in the VOD
    (
        'seed-completed-001',
        'viewer123',
        'Lets go! This is going to be epic! PogChamp',
        NOW() - INTERVAL '2 days' + INTERVAL '10 seconds',
        10.0,
        'subscriber/12',
        '25:0-7',
        '#FF6347',
        NULL, NULL, NULL,
        '',
        NOW() - INTERVAL '2 days' + INTERVAL '10 seconds'
    ),
    (
        'seed-completed-001',
        'moderator_mike',
        'Welcome everyone! Remember to follow the rules',
        NOW() - INTERVAL '2 days' + INTERVAL '45 seconds',
        45.0,
        'moderator/1,subscriber/24',
        NULL,
        '#00FF00',
        NULL, NULL, NULL,
        '',
        NOW() - INTERVAL '2 days' + INTERVAL '45 seconds'
    ),
    (
        'seed-completed-001',
        'gamerfan99',
        'That boss mechanic is so tough!',
        NOW() - INTERVAL '2 days' + INTERVAL '15 minutes',
        900.0,
        'subscriber/6',
        NULL,
        '#9B59B6',
        NULL, NULL, NULL,
        '',
        NOW() - INTERVAL '2 days' + INTERVAL '15 minutes'
    ),
    (
        'seed-completed-001',
        'streamer_pro',
        'You got this! Focus on the patterns',
        NOW() - INTERVAL '2 days' + INTERVAL '15 minutes 20 seconds',
        920.0,
        'subscriber/36',
        NULL,
        '#FFD700',
        NULL, NULL, NULL,
        '',
        NOW() - INTERVAL '2 days' + INTERVAL '15 minutes 20 seconds'
    ),
    (
        'seed-completed-001',
        'viewer123',
        'YESSSS! You did it! Kreygasm',
        NOW() - INTERVAL '2 days' + INTERVAL '32 minutes',
        1920.0,
        'subscriber/12',
        '41:0-6',
        '#FF6347',
        NULL, NULL, NULL,
        '',
        NOW() - INTERVAL '2 days' + INTERVAL '32 minutes'
    ),
    (
        'seed-completed-001',
        'chatbot_helper',
        'Streamer has been live for 30 minutes! Drop a follow if you are enjoying the stream!',
        NOW() - INTERVAL '2 days' + INTERVAL '30 minutes',
        1800.0,
        'bot/1',
        NULL,
        '#0000FF',
        NULL, NULL, NULL,
        '',
        NOW() - INTERVAL '2 days' + INTERVAL '30 minutes'
    ),
    (
        'seed-completed-001',
        'lucky_viewer',
        'This is my first time watching, amazing content!',
        NOW() - INTERVAL '2 days' + INTERVAL '45 minutes',
        2700.0,
        NULL,
        NULL,
        '#808080',
        NULL, NULL, NULL,
        '',
        NOW() - INTERVAL '2 days' + INTERVAL '45 minutes'
    ),
    (
        'seed-completed-001',
        'moderator_mike',
        '@lucky_viewer Welcome! Glad you are here!',
        NOW() - INTERVAL '2 days' + INTERVAL '45 minutes 15 seconds',
        2715.0,
        'moderator/1,subscriber/24',
        NULL,
        '#00FF00',
        NULL,
        'lucky_viewer',
        'This is my first time watching, amazing content!',
        '',
        NOW() - INTERVAL '2 days' + INTERVAL '45 minutes 15 seconds'
    ),
    (
        'seed-completed-001',
        'subscriber_alice',
        'This is the content I subbed for! <3',
        NOW() - INTERVAL '2 days' + INTERVAL '1 hour',
        3600.0,
        'subscriber/18',
        NULL,
        '#FF1493',
        NULL, NULL, NULL,
        '',
        NOW() - INTERVAL '2 days' + INTERVAL '1 hour'
    ),
    (
        'seed-completed-001',
        'viewer123',
        'How many attempts did this boss take in previous streams?',
        NOW() - INTERVAL '2 days' + INTERVAL '1 hour 15 minutes',
        4500.0,
        'subscriber/12',
        NULL,
        '#FF6347',
        NULL, NULL, NULL,
        '',
        NOW() - INTERVAL '2 days' + INTERVAL '1 hour 15 minutes'
    ),
    (
        'seed-completed-001',
        'gamerfan99',
        'I think it was like 20+ attempts last time',
        NOW() - INTERVAL '2 days' + INTERVAL '1 hour 15 minutes 30 seconds',
        4530.0,
        'subscriber/6',
        NULL,
        '#9B59B6',
        NULL,
        'viewer123',
        'How many attempts did this boss take in previous streams?',
        '',
        NOW() - INTERVAL '2 days' + INTERVAL '1 hour 15 minutes 30 seconds'
    ),
    (
        'seed-completed-001',
        'epic_gamer_2024',
        'The RNG on this fight is brutal LUL',
        NOW() - INTERVAL '2 days' + INTERVAL '1 hour 45 minutes',
        6300.0,
        'subscriber/3',
        '88:30-32',
        '#FF8C00',
        NULL, NULL, NULL,
        '',
        NOW() - INTERVAL '2 days' + INTERVAL '1 hour 45 minutes'
    ),
    (
        'seed-completed-001',
        'streamer_pro',
        'Final phase coming up! This is going to be intense!',
        NOW() - INTERVAL '2 days' + INTERVAL '1 hour 55 minutes',
        6900.0,
        'subscriber/36',
        NULL,
        '#FFD700',
        NULL, NULL, NULL,
        '',
        NOW() - INTERVAL '2 days' + INTERVAL '1 hour 55 minutes'
    ),
    (
        'seed-completed-001',
        'hype_train_conductor',
        'HYPE HYPE HYPE PogChamp PogChamp PogChamp',
        NOW() - INTERVAL '2 days' + INTERVAL '1 hour 58 minutes',
        7080.0,
        'subscriber/9',
        '25:5-12,25:13-20,25:21-28',
        '#FF0000',
        NULL, NULL, NULL,
        '',
        NOW() - INTERVAL '2 days' + INTERVAL '1 hour 58 minutes'
    ),
    (
        'seed-completed-001',
        'everyone',
        'GG! What an amazing run! Clap Clap Clap',
        NOW() - INTERVAL '2 days' + INTERVAL '2 hours 30 seconds',
        7230.0,
        'subscriber/1',
        '100:0-3,100:4-7,100:8-11',
        '#FFFFFF',
        NULL, NULL, NULL,
        '',
        NOW() - INTERVAL '2 days' + INTERVAL '2 hours 30 seconds'
    );

-- Insert some KV entries for testing circuit breaker and stats
INSERT INTO kv (channel, key, value, updated_at) VALUES
    ('', 'circuit_state', 'closed', NOW() - INTERVAL '1 hour'),
    ('', 'circuit_failures', '0', NOW() - INTERVAL '1 hour'),
    ('', 'circuit_open_until', '', NOW() - INTERVAL '1 hour'),
    ('', 'avg_download_ms', '45000', NOW() - INTERVAL '30 minutes'),
    ('', 'avg_upload_ms', '120000', NOW() - INTERVAL '30 minutes'),
    ('', 'last_catalog_refresh', (NOW() - INTERVAL '3 hours')::text, NOW() - INTERVAL '3 hours'),
    ('', 'seed_data_loaded', 'true', NOW());

COMMIT;

-- Display summary
SELECT 'Seed data loaded successfully!' as status;
SELECT COUNT(*) as vod_count FROM vods WHERE twitch_vod_id LIKE 'seed-%';
SELECT COUNT(*) as chat_message_count FROM chat_messages WHERE vod_id LIKE 'seed-%';
SELECT COUNT(*) as kv_entry_count FROM kv WHERE key LIKE 'circuit_%' OR key LIKE 'avg_%';
