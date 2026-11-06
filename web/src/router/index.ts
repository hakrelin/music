import { createRouter, createWebHistory } from 'vue-router';

const router = createRouter({
  history: createWebHistory(),
  scrollBehavior(to, from, saved) {
    if (saved) return saved;
    if (to.hash) return { el: to.hash, behavior: 'smooth' };
    return { top: 0 };
  },
  routes: [
    {
      path: '/',
      name: 'home',
      component: () => import('@/views/HomeView.vue')
    },
    {
      path: '/room/:id',
      name: 'room',
      component: () => import('@/views/RoomView.vue'),
      props: true
    },
    { path: '/:pathMatch(.*)*', redirect: '/' }
  ]
});

export default router;
