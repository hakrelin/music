import Vue from 'vue'
import VueRouter from 'vue-router'

Vue.use(VueRouter);

const routes = [
  {
    path: '/',
    name: 'lobby',
    component: () => import(/* webpackChunkName: "lobby" */ '../views/Lobby.vue')
  },
  {
    path: '/room/:houseId',
    name: 'music',
    component: () => import(/* webpackChunkName: "music" */ '../views/Music.vue')
  },
  { path: '*', redirect: '/' }
];

const router = new VueRouter({
  // mode: 'history',
  base: '/',
  routes
});

export default router;
