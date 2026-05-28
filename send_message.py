import requests
import json

url = 'http://127.0.0.1:8000/api/v1/ai-planning/sessions/255/messages'
content = '账号Xjy13302412005@outlook.com密码123456筛选Polo前两件商品加入购物车验证价格'

response = requests.post(url, json={'content': content}, timeout=180)
print(response.text[:1000])
