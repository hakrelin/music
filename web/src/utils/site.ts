/**
 * 备案号按访问域名挑（一个备案主体下有多个网站序号）。
 *
 * 同一个站挂在多个域名上：
 *   *.tea-kompeito.com → 苏ICP备2026082134号-2
 *   *.tea-sugar.xyz    → 苏ICP备2026082134号-1
 *   *.northwind.site   → 朋友自己的域名，我们不知道它备案到哪个号，先不渲染
 *                        （要显示就把它的号加进 CASE 表）
 */
export type IcpInfo = { text: string; href: string };

const BEIAN_URL = 'https://beian.miit.gov.cn/';

const CASES: { suffix: string; text: string }[] = [
  { suffix: 'tea-kompeito.com', text: '苏ICP备2026082134号-2' },
  { suffix: 'tea-sugar.xyz', text: '苏ICP备2026082134号-1' },
  // northwind.site 是朋友的域名（湘ICP备2022020010号-1），他本人不想让它承载本站内容，
  // nginx 已经把它 301 到 music.tea-kompeito.com，所以这里不放开。
  // 哪天要恢复独立入口：nginx 换回 musicd-proxy.inc + 取消下面这行注释。
  // { suffix: 'northwind.site', text: '湘ICP备2022020010号-1' },
];

export function icpForHost(host: string = location.hostname): IcpInfo | null {
  const h = (host || '').toLowerCase().replace(/:\d+$/, '');
  for (const c of CASES) {
    if (h === c.suffix || h.endsWith('.' + c.suffix)) return { text: c.text, href: BEIAN_URL };
  }
  return null;
}
