// 本地克隆用的配置（原仓库只有 publicPath 一行）
module.exports = {
    publicPath: './',

    // dev server 跑 8081；8080 留给 mock 后端（environment.js 里 dev 的 baseUrl 就是 127.0.0.1:8080）
    devServer: {
        port: 8081,
        host: '127.0.0.1',
        hot: true,
        overlay: { warnings: false, errors: true }
    },

    // node-sass 4 在 Node 22 上装不上，换 dart-sass；sass-loader 8 需要显式 implementation
    css: {
        loaderOptions: {
            sass: { implementation: require('sass') }
        }
    },

    productionSourceMap: false
};
