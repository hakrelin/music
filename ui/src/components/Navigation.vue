<template>
    <div>
        <mu-appbar style="width: 100%;" color="primary" z-depth="1">
            <mu-button icon slot="left" @click="goLobby" title="返回房间列表">
                <mu-icon value="arrow_back"></mu-icon>
            </mu-button>
            <span class="nav-title">{{ title }}</span>
            <mu-button flat slot="right" @click="editNick" title="点击修改昵称">
                {{ nick }}
            </mu-button>
        </mu-appbar>
    </div>
</template>

<script>
    import { getNick, setNick } from '../services/nick';

    export default {
        name: "Navigation",
        data: () => ({
            nick: getNick(),
            isOpenDrawer: false
        }),
        computed: {
            title() {
                const rn = this.$store.getters.getRoomName;
                return rn || '一起听歌吧';
            }
        },
        methods: {
            goLobby() {
                this.$router.push('/');
            },
            editNick() {
                const n = window.prompt('改个昵称（所有房间通用）', this.nick);
                if (!n || !n.trim()) return;
                this.nick = n.trim().slice(0, 16);
                setNick(this.nick);
                // 立刻同步给当前房间
                const c = this.$store.getters.getStompClient;
                if (c && c.connected) {
                    c.send('/setting/name', {}, JSON.stringify({ name: this.nick, sendTime: Date.now() }));
                }
            }
        }
    }
</script>

<style scoped>
    .nav-title {
        font-size: 16px;
        letter-spacing: .5px;
        white-space: nowrap;
        overflow: hidden;
        text-overflow: ellipsis;
        max-width: 50vw;
    }
</style>
