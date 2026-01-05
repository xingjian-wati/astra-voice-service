/**
 * LiveKit 测试工具类
 * 用于测试 LiveKit 集成功能
 */

class LiveKitTester {
    constructor(apiBaseUrl = 'http://localhost:8082') {
        this.apiBaseUrl = apiBaseUrl;
        this.room = null;
        this.localAudioTrack = null;
        this.connectionId = null;
        this.listeners = {
            onLog: [],
            onStatusChange: [],
            onAudioReceived: []
        };
    }

    /**
     * 添加事件监听器
     */
    on(event, callback) {
        if (this.listeners[event]) {
            this.listeners[event].push(callback);
        }
    }

    /**
     * 触发事件
     */
    emit(event, ...args) {
        if (this.listeners[event]) {
            this.listeners[event].forEach(cb => cb(...args));
        }
    }

    /**
     * 日志记录
     */
    log(message, level = 'info') {
        const timestamp = new Date().toISOString();
        console.log(`[${timestamp}] [${level}] ${message}`);
        this.emit('onLog', { message, level, timestamp });
    }

    /**
     * 状态更新
     */
    updateStatus(status, message) {
        this.log(`Status: ${status} - ${message}`, 'info');
        this.emit('onStatusChange', { status, message });
    }

    /**
     * 测试麦克风权限
     */
    async testMicrophone() {
        try {
            this.log('Testing microphone permissions...', 'info');
            const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
            
            // 测试音频电平
            const audioContext = new AudioContext();
            const analyser = audioContext.createAnalyser();
            const microphone = audioContext.createMediaStreamSource(stream);
            microphone.connect(analyser);
            analyser.fftSize = 256;
            
            const dataArray = new Uint8Array(analyser.frequencyBinCount);
            
            return new Promise((resolve) => {
                let samples = 0;
                const interval = setInterval(() => {
                    analyser.getByteFrequencyData(dataArray);
                    const average = dataArray.reduce((a, b) => a + b) / dataArray.length;
                    this.log(`Microphone level: ${Math.round(average)}/255`, 'info');
                    
                    samples++;
                    if (samples >= 3) {
                        clearInterval(interval);
                        stream.getTracks().forEach(track => track.stop());
                        this.log('Microphone test completed successfully', 'success');
                        resolve(true);
                    }
                }, 1000);
            });
        } catch (error) {
            this.log(`Microphone test failed: ${error.message}`, 'error');
            throw error;
        }
    }

    /**
     * 创建房间并获取 token
     */
    async createRoom(participantName, agentId = 'agent-1', voiceLanguage = 'en') {
        try {
            this.log(`Creating room for ${participantName}...`, 'info');
            
            const response = await fetch(`${this.apiBaseUrl}/livekit/create-room`, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify({
                    participantName,
                    agentId,
                    voiceLanguage
                })
            });

            if (!response.ok) {
                const errorText = await response.text();
                throw new Error(`API error (${response.status}): ${errorText}`);
            }

            const data = await response.json();
            this.connectionId = data.connectionId;
            
            this.log(`Room created: ${data.roomName}`, 'success');
            this.log(`Connection ID: ${data.connectionId}`, 'info');
            
            return data;
        } catch (error) {
            this.log(`Failed to create room: ${error.message}`, 'error');
            throw error;
        }
    }

    /**
     * 连接到 LiveKit 房间
     */
    async connect(serverUrl, accessToken) {
        try {
            this.log('Connecting to LiveKit server...', 'info');
            
            // 创建房间实例
            this.room = new LivekitClient.Room();

            // 设置事件监听
            this.setupRoomEvents();

            // 连接到房间
            await this.room.connect(serverUrl, accessToken);
            this.log('Connected to LiveKit room successfully', 'success');
            this.updateStatus('connected', 'Connected to room');

            // 发布本地音频
            await this.publishLocalAudio();

            return true;
        } catch (error) {
            this.log(`Connection failed: ${error.message}`, 'error');
            throw error;
        }
    }

    /**
     * 设置房间事件监听
     */
    setupRoomEvents() {
        this.room.on(LivekitClient.RoomEvent.Connected, () => {
            this.log('Room connected event fired', 'success');
        });

        this.room.on(LivekitClient.RoomEvent.TrackSubscribed, (track, publication, participant) => {
            this.log(`Track subscribed: ${track.kind} from ${participant.identity}`, 'success');
            
            if (track.kind === 'audio') {
                this.log('Audio track received from AI', 'success');
                this.emit('onAudioReceived', track);
            }
        });

        this.room.on(LivekitClient.RoomEvent.TrackUnsubscribed, (track) => {
            this.log(`Track unsubscribed: ${track.kind}`, 'info');
        });

        this.room.on(LivekitClient.RoomEvent.Disconnected, () => {
            this.log('Disconnected from room', 'warning');
            this.updateStatus('disconnected', 'Disconnected');
        });

        this.room.on(LivekitClient.RoomEvent.ParticipantConnected, (participant) => {
            this.log(`Participant joined: ${participant.identity}`, 'info');
        });

        this.room.on(LivekitClient.RoomEvent.ParticipantDisconnected, (participant) => {
            this.log(`Participant left: ${participant.identity}`, 'info');
        });

        this.room.on(LivekitClient.RoomEvent.Reconnecting, () => {
            this.log('Reconnecting...', 'warning');
            this.updateStatus('reconnecting', 'Reconnecting to server');
        });

        this.room.on(LivekitClient.RoomEvent.Reconnected, () => {
            this.log('Reconnected successfully', 'success');
            this.updateStatus('connected', 'Reconnected to server');
        });
    }

    /**
     * 发布本地音频轨道
     */
    async publishLocalAudio() {
        try {
            this.log('Publishing local audio track...', 'info');
            
            this.localAudioTrack = await LivekitClient.createLocalAudioTrack({
                echoCancellation: true,
                noiseSuppression: true,
                autoGainControl: true,
                // Optimize for low latency
                latency: 0,
                sampleRate: 48000,
                channelCount: 1
            });

            // ✅ 生产模式：禁用 DTX 以获得最佳 VAD 性能和最低延迟
            await this.room.localParticipant.publishTrack(this.localAudioTrack, {
                dtx: false,  // 🔥 禁用 DTX，确保音频包持续发送，OpenAI VAD 快速响应
                audioBitrate: 32000,  // 32kbps，与服务端一致
            });
            
            this.log('Local audio published successfully', 'success');
            return true;
        } catch (error) {
            this.log(`Failed to publish local audio: ${error.message}`, 'error');
            throw error;
        }
    }

    /**
     * 断开连接
     */
    async disconnect() {
        try {
            this.log('Disconnecting...', 'info');

            // 停止本地音频
            if (this.localAudioTrack) {
                this.localAudioTrack.stop();
                this.localAudioTrack = null;
            }

            // 断开房间连接
            if (this.room) {
                this.room.disconnect();
                this.room = null;
            }

            // 调用后端 API 清理
            if (this.connectionId) {
                await this.endCall();
            }

            this.log('Disconnected successfully', 'success');
            this.updateStatus('disconnected', 'Disconnected');
            
            return true;
        } catch (error) {
            this.log(`Error during disconnect: ${error.message}`, 'error');
            throw error;
        }
    }

    /**
     * 调用后端 API 结束通话
     */
    async endCall() {
        try {
            this.log(`Ending call ${this.connectionId}...`, 'info');
            
            const response = await fetch(`${this.apiBaseUrl}/livekit/end-call`, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify({
                    connectionId: this.connectionId
                })
            });

            if (!response.ok) {
                throw new Error(`Failed to end call: ${response.status}`);
            }

            const data = await response.json();
            this.log('Call ended on server', 'success');
            this.connectionId = null;
            
            return data;
        } catch (error) {
            this.log(`Failed to end call: ${error.message}`, 'warning');
            // 不抛出错误，因为本地已经断开
        }
    }

    /**
     * 获取连接状态
     */
    async getConnectionStatus() {
        if (!this.connectionId) {
            throw new Error('No active connection');
        }

        try {
            const response = await fetch(
                `${this.apiBaseUrl}/livekit/connection-status/${this.connectionId}`
            );

            if (!response.ok) {
                throw new Error(`Failed to get status: ${response.status}`);
            }

            return await response.json();
        } catch (error) {
            this.log(`Failed to get connection status: ${error.message}`, 'error');
            throw error;
        }
    }

    /**
     * 获取统计信息
     */
    async getStats() {
        try {
            const response = await fetch(`${this.apiBaseUrl}/livekit/stats`);

            if (!response.ok) {
                throw new Error(`Failed to get stats: ${response.status}`);
            }

            return await response.json();
        } catch (error) {
            this.log(`Failed to get stats: ${error.message}`, 'error');
            throw error;
        }
    }

    /**
     * 完整的测试流程
     */
    async runFullTest(participantName, agentId = 'agent-1', voiceLanguage = 'en') {
        try {
            this.log('=== Starting full LiveKit test ===', 'info');

            // 1. 测试麦克风
            this.log('Step 1: Testing microphone...', 'info');
            await this.testMicrophone();

            // 2. 创建房间
            this.log('Step 2: Creating room...', 'info');
            const roomData = await this.createRoom(participantName, agentId, voiceLanguage);

            // 3. 连接到房间
            this.log('Step 3: Connecting to room...', 'info');
            await this.connect(roomData.serverUrl, roomData.accessToken);

            this.log('=== Full test completed successfully ===', 'success');
            return true;
        } catch (error) {
            this.log(`=== Test failed: ${error.message} ===`, 'error');
            throw error;
        }
    }
}

// 导出为全局变量（用于浏览器控制台测试）
if (typeof window !== 'undefined') {
    window.LiveKitTester = LiveKitTester;
}

// 导出为模块（用于 Node.js 或模块化项目）
if (typeof module !== 'undefined' && module.exports) {
    module.exports = LiveKitTester;
}

