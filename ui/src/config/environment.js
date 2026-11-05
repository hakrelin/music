/**
 * 环境配置
 *
 * development: 本地 mock 后端（mock_server.py 跑在 8080）
 * production : 与页面同源（jar 自己既出静态又出接口，nginx 反代同域名即可）
 *
 * @type {string}
 */

let baseUrl = 'http://127.0.0.1';

if (process.env.NODE_ENV === "development") {
    baseUrl = "http://127.0.0.1:8080";
} else {
    // 生产/测试都走同源，避免把 IP 写死
    baseUrl = (typeof window !== 'undefined' && window.location)
        ? window.location.origin
        : '';
}

let isProduction = process.env.NODE_ENV === "production";

export {
    baseUrl,
    isProduction
}
