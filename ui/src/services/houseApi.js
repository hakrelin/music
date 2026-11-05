/**
 * 房间相关接口
 * 注意：线上这个 jar（2024-11 构建）只映射了 POST，
 * GET /house/search 会落到 Spring 的 401，所以统一用 POST。
 */
import { baseUrl } from '../config/environment';

async function post(path, body) {
    const r = await fetch(baseUrl + path, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body || {})
    });
    if (!r.ok) {
        throw new Error('HTTP ' + r.status);
    }
    const text = await r.text();
    return text ? JSON.parse(text) : {};
}

/** 房间列表 */
export function searchHouses() {
    return post('/house/search', {});
}

/** 创建房间 {name, desc, needPwd, password} → data = houseId */
export function addHouse(house) {
    return post('/house/add', house);
}

/** 进入房间校验 {id, password} */
export function enterHouse(id, password) {
    return post('/house/enter', { id, password });
}
